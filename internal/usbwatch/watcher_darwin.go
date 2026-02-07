package usbwatch

import (
	"context"
	"log"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// CF and IOKit type aliases matching usbhid conventions.
type (
	cfAllocatorRef  uintptr
	cfDictionaryRef uintptr
	cfIndex         int64
	cfNumberRef     uintptr
	cfNumberType    = cfIndex
	cfRunLoopRef    uintptr
	cfStringRef     uintptr
	cfTypeRef       uintptr

	cfStringEncoding uint32

	ioHIDDeviceRef  uintptr
	ioHIDManagerRef uintptr
	ioOptionBits    uint32
	ioReturn        int32
)

const (
	kCFAllocatorDefault   cfAllocatorRef  = 0
	kCFNumberSInt16Type   cfIndex         = 2
	kCFStringEncodingUTF8 cfStringEncoding = 0x08000100

	kIOHIDOptionsTypeNone ioOptionBits = 0
	kIOReturnSuccess      ioReturn     = 0
)

// purego function bindings
var (
	cfNumberGetValue        func(number cfNumberRef, theType cfNumberType, valuePtr unsafe.Pointer) bool
	cfRelease               func(cf cfTypeRef)
	cfRunLoopGetCurrent     func() cfRunLoopRef
	cfRunLoopRun            func()
	cfRunLoopStop           func(runLoop cfRunLoopRef)
	cfStringCreateWithBytes func(alloc cfAllocatorRef, bytes []byte, numBytes cfIndex, encoding cfStringEncoding, isExternalRepresentation bool) cfStringRef

	ioHIDDeviceGetProperty                 func(device ioHIDDeviceRef, key cfStringRef) cfTypeRef
	ioHIDManagerClose                      func(manager ioHIDManagerRef, options ioOptionBits) ioReturn
	ioHIDManagerCreate                     func(allocator cfAllocatorRef, options ioOptionBits) ioHIDManagerRef
	ioHIDManagerOpen                       func(manager ioHIDManagerRef, options ioOptionBits) ioReturn
	ioHIDManagerSetDeviceMatching          func(manager ioHIDManagerRef, matching cfDictionaryRef)
	ioHIDManagerRegisterDeviceMatchingCallback func(manager ioHIDManagerRef, callback uintptr, context unsafe.Pointer)
	ioHIDManagerScheduleWithRunLoop        func(manager ioHIDManagerRef, runLoop cfRunLoopRef, runLoopMode cfStringRef)
)

var kCFRunLoopDefaultMode uintptr

func init() {
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}

	purego.RegisterLibFunc(&cfNumberGetValue, cf, "CFNumberGetValue")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&cfRunLoopGetCurrent, cf, "CFRunLoopGetCurrent")
	purego.RegisterLibFunc(&cfRunLoopRun, cf, "CFRunLoopRun")
	purego.RegisterLibFunc(&cfRunLoopStop, cf, "CFRunLoopStop")
	purego.RegisterLibFunc(&cfStringCreateWithBytes, cf, "CFStringCreateWithBytes")

	kCFRunLoopDefaultMode, err = purego.Dlsym(cf, "kCFRunLoopDefaultMode")
	if err != nil {
		panic(err)
	}

	iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}

	purego.RegisterLibFunc(&ioHIDDeviceGetProperty, iokit, "IOHIDDeviceGetProperty")
	purego.RegisterLibFunc(&ioHIDManagerClose, iokit, "IOHIDManagerClose")
	purego.RegisterLibFunc(&ioHIDManagerCreate, iokit, "IOHIDManagerCreate")
	purego.RegisterLibFunc(&ioHIDManagerOpen, iokit, "IOHIDManagerOpen")
	purego.RegisterLibFunc(&ioHIDManagerSetDeviceMatching, iokit, "IOHIDManagerSetDeviceMatching")
	purego.RegisterLibFunc(&ioHIDManagerRegisterDeviceMatchingCallback, iokit, "IOHIDManagerRegisterDeviceMatchingCallback")
	purego.RegisterLibFunc(&ioHIDManagerScheduleWithRunLoop, iokit, "IOHIDManagerScheduleWithRunLoop")
}

// watcherCtx holds the state passed to the IOKit callback.
// A Go-side reference is kept alive to prevent GC.
type watcherCtx struct {
	ch       chan<- struct{}
	vendorID uint16
}

func deviceMatchingCallback(_ unsafe.Pointer, _ ioReturn, _ uintptr, device ioHIDDeviceRef) {
	if callbackCtx == nil {
		return
	}

	vid, ok := getDeviceVendorID(device)
	if !ok {
		return
	}

	if vid != callbackCtx.vendorID {
		return
	}

	log.Printf("USB device arrived (vendor 0x%04x)", vid)
	select {
	case callbackCtx.ch <- struct{}{}:
	default:
	}
}

// callbackCtx is the package-level reference to the watcher context.
// Kept here so the GC doesn't collect it while the callback is registered.
// Only one watcher is supported at a time.
var callbackCtx *watcherCtx

var deviceMatchingCallbackPtr = purego.NewCallback(deviceMatchingCallback)

func getDeviceVendorID(device ioHIDDeviceRef) (uint16, bool) {
	key := []byte("VendorID")
	skey := cfStringCreateWithBytes(kCFAllocatorDefault, key, cfIndex(len(key)), kCFStringEncodingUTF8, false)
	if skey == 0 {
		return 0, false
	}
	defer cfRelease(cfTypeRef(skey))

	prop := ioHIDDeviceGetProperty(device, skey)
	if prop == 0 {
		return 0, false
	}

	var vid uint16
	if !cfNumberGetValue(cfNumberRef(prop), kCFNumberSInt16Type, unsafe.Pointer(&vid)) {
		return 0, false
	}
	return vid, true
}

// Watch returns a channel that receives a signal each time a USB HID device
// with the given vendor ID appears on the bus. Uses IOKit's device matching
// callback for zero-CPU-cost waiting. The watcher stops when ctx is cancelled.
func Watch(ctx context.Context, vendorID uint16) <-chan struct{} {
	ch := make(chan struct{}, 1)

	wctx := &watcherCtx{
		ch:       ch,
		vendorID: vendorID,
	}
	callbackCtx = wctx

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		mgr := ioHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone)
		if rv := ioHIDManagerOpen(mgr, kIOHIDOptionsTypeNone); rv != kIOReturnSuccess {
			log.Printf("usbwatch: failed to open IOHIDManager: 0x%08x", rv)
			return
		}

		// Match all HID devices; we filter by vendor ID in the callback.
		ioHIDManagerSetDeviceMatching(mgr, 0)

		rl := cfRunLoopGetCurrent()
		ioHIDManagerScheduleWithRunLoop(mgr, rl, **(**cfStringRef)(unsafe.Pointer(&kCFRunLoopDefaultMode)))
		ioHIDManagerRegisterDeviceMatchingCallback(mgr, deviceMatchingCallbackPtr, nil)

		// Stop the run loop when the context is cancelled.
		go func() {
			<-ctx.Done()
			cfRunLoopStop(rl)
		}()

		log.Println("usbwatch: listening for USB HID device arrivals")
		cfRunLoopRun()

		ioHIDManagerClose(mgr, kIOHIDOptionsTypeNone)
		cfRelease(cfTypeRef(mgr))
		callbackCtx = nil
		log.Println("usbwatch: stopped")
	}()

	return ch
}
