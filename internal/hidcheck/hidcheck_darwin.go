// Package hidcheck counts the kernel IOHIDLibUserClient objects this process
// holds, so a leak in the HID stack shows up in the log long before it wedges
// the machine.
//
// Every IOHIDDeviceOpen creates one of these in the kernel and it lives until
// the matching close (or the process exits). A library bug that opens devices
// without closing them is invisible from userspace: the process looks healthy,
// the deck works, and the count climbs until the kernel HID stack stops
// answering and the built-in keyboard dies after the next wake. That happened
// twice before anyone thought to look at ioreg, so now we look ourselves.
package hidcheck

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

type (
	cfAllocatorRef   uintptr
	cfDictionaryRef  uintptr
	cfIndex          int64
	cfStringEncoding uint32
	cfStringRef      uintptr
	cfTypeRef        uintptr

	ioIterator   uint32
	ioObject     uint32
	ioOptionBits uint32
	kernReturn   int32
)

const (
	kCFAllocatorDefault   cfAllocatorRef   = 0
	kCFStringEncodingUTF8 cfStringEncoding = 0x08000100
	kIOMainPortDefault    uint32           = 0
	kernSuccess           kernReturn       = 0

	kIORegistryIterateRecursively ioOptionBits = 1
	ioServicePlane                             = "IOService"

	userClientClass = "IOHIDLibUserClient"
	creatorProperty = "IOUserClientCreator"
)

var (
	cfRelease               func(cf cfTypeRef)
	cfStringCreateWithBytes func(alloc cfAllocatorRef, bytes []byte, numBytes cfIndex, encoding cfStringEncoding, isExternalRepresentation bool) cfStringRef
	cfStringGetCString      func(str cfStringRef, buffer []byte, bufferSize cfIndex, encoding cfStringEncoding) bool

	ioIteratorNext                  func(iterator ioIterator) ioObject
	ioObjectConformsTo              func(object ioObject, className string) uint32
	ioObjectRelease                 func(object ioObject) kernReturn
	ioRegistryCreateIterator        func(mainPort uint32, plane string, options ioOptionBits, iterator *ioIterator) kernReturn
	ioRegistryEntryCreateCFProperty func(entry ioObject, key cfStringRef, allocator cfAllocatorRef, options ioOptionBits) cfTypeRef
)

func init() {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&cfStringCreateWithBytes, cf, "CFStringCreateWithBytes")
	purego.RegisterLibFunc(&cfStringGetCString, cf, "CFStringGetCString")

	iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&ioIteratorNext, iokit, "IOIteratorNext")
	purego.RegisterLibFunc(&ioObjectConformsTo, iokit, "IOObjectConformsTo")
	purego.RegisterLibFunc(&ioObjectRelease, iokit, "IOObjectRelease")
	purego.RegisterLibFunc(&ioRegistryCreateIterator, iokit, "IORegistryCreateIterator")
	purego.RegisterLibFunc(&ioRegistryEntryCreateCFProperty, iokit, "IORegistryEntryCreateCFProperty")
}

// Counts is a snapshot of the HID user clients in the kernel.
type Counts struct {
	// Total is every IOHIDLibUserClient on the machine, whoever created it.
	Total int
	// Ours is the subset whose IOUserClientCreator names this process.
	Ours int
}

// Count walks the IOKit registry for IOHIDLibUserClient objects and reports
// how many there are and how many this process created. The registry walk is
// a few milliseconds; it is fine to call after every device probe.
//
// This walks the whole IOService plane the way ioreg does rather than using
// IOServiceGetMatchingServices, because user clients are attached to the
// registry without being registered as services and the matching API never
// returns them.
func Count() (Counts, error) {
	var iter ioIterator
	if rv := ioRegistryCreateIterator(kIOMainPortDefault, ioServicePlane, kIORegistryIterateRecursively, &iter); rv != kernSuccess {
		return Counts{}, fmt.Errorf("hidcheck: IORegistryCreateIterator: 0x%08x", uint32(rv))
	}
	defer ioObjectRelease(ioObject(iter))

	keyBytes := []byte(creatorProperty)
	key := cfStringCreateWithBytes(kCFAllocatorDefault, keyBytes, cfIndex(len(keyBytes)), kCFStringEncodingUTF8, false)
	if key == 0 {
		return Counts{}, fmt.Errorf("hidcheck: failed to create CFString for %q", creatorProperty)
	}
	defer cfRelease(cfTypeRef(key))

	// The creator string looks like "pid 842, belowdeck".
	pidPrefix := fmt.Sprintf("pid %d,", os.Getpid())

	var counts Counts
	buf := make([]byte, 256)
	for {
		entry := ioIteratorNext(iter)
		if entry == 0 {
			break
		}
		if ioObjectConformsTo(entry, userClientClass) == 0 {
			ioObjectRelease(entry)
			continue
		}
		counts.Total++

		if creator := ioRegistryEntryCreateCFProperty(entry, key, kCFAllocatorDefault, 0); creator != 0 {
			if cfStringGetCString(cfStringRef(creator), buf, cfIndex(len(buf)), kCFStringEncodingUTF8) {
				s := string(buf[:strings.IndexByte(string(buf), 0)])
				if strings.HasPrefix(s, pidPrefix) {
					counts.Ours++
				}
			}
			cfRelease(creator)
		}
		ioObjectRelease(entry)
	}

	return counts, nil
}

// WarnThreshold is the number of user clients held by this process above
// which Check starts complaining. A healthy belowdeck holds a handful: one
// for the usbwatch manager's view of the deck and a couple while the deck is
// open. Anything in the dozens means something is opening devices and not
// closing them.
const WarnThreshold = 50

var (
	warnMtx    sync.Mutex
	warnedHigh int
)

// Check counts user clients and logs a warning when this process holds more
// than WarnThreshold of them. It only logs again once the count has grown
// past the last warned value, so a stable count produces one line and a
// climbing one produces a trajectory you can read off the log.
func Check() {
	counts, err := Count()
	if err != nil {
		log.Printf("hidcheck: %v", err)
		return
	}

	warnMtx.Lock()
	defer warnMtx.Unlock()

	if counts.Ours < WarnThreshold {
		warnedHigh = 0
		return
	}
	if counts.Ours <= warnedHigh {
		return
	}
	warnedHigh = counts.Ours
	log.Printf("WARNING: this process holds %d IOHIDLibUserClient objects (%d on the machine); something is opening HID devices without closing them", counts.Ours, counts.Total)
}

// Log writes the current counts at info level. Meant for moments that happen
// a few times a day (device close, wake), not for every probe.
func Log(when string) {
	counts, err := Count()
	if err != nil {
		log.Printf("hidcheck: %v", err)
		return
	}
	log.Printf("HID user clients %s: %d held by this process, %d on the machine", when, counts.Ours, counts.Total)
}
