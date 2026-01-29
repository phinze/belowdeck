package device

import (
	"image"
	"time"

	"rafaelmartins.com/p/streamdeck"
)

// HardwareDevice wraps the real streamdeck.Device to implement the Device interface.
type HardwareDevice struct {
	dev *streamdeck.Device
}

// NewHardware creates a new hardware device wrapper.
func NewHardware(dev *streamdeck.Device) *HardwareDevice {
	return &HardwareDevice{dev: dev}
}

// Open opens the device for use.
func (h *HardwareDevice) Open() error {
	return h.dev.Open()
}

// Close closes the device.
func (h *HardwareDevice) Close() error {
	return h.dev.Close()
}

// IsOpen returns whether the device is open.
func (h *HardwareDevice) IsOpen() bool {
	return h.dev.IsOpen()
}

// GetModelName returns the device model name.
func (h *HardwareDevice) GetModelName() string {
	return h.dev.GetModelName()
}

// GetKeyCount returns the number of keys on the device.
func (h *HardwareDevice) GetKeyCount() byte {
	return h.dev.GetKeyCount()
}

// GetDialCount returns the number of dials on the device.
func (h *HardwareDevice) GetDialCount() byte {
	return h.dev.GetDialCount()
}

// GetTouchStripSupported returns whether the device has a touch strip.
func (h *HardwareDevice) GetTouchStripSupported() bool {
	return h.dev.GetTouchStripSupported()
}

// GetKeyImageRectangle returns the dimensions for key images.
func (h *HardwareDevice) GetKeyImageRectangle() (image.Rectangle, error) {
	return h.dev.GetKeyImageRectangle()
}

// GetTouchStripImageRectangle returns the dimensions for the touch strip image.
func (h *HardwareDevice) GetTouchStripImageRectangle() (image.Rectangle, error) {
	return h.dev.GetTouchStripImageRectangle()
}

// SetBrightness sets the device brightness.
func (h *HardwareDevice) SetBrightness(perc byte) error {
	return h.dev.SetBrightness(perc)
}

// SetKeyImage sets the image for a key.
func (h *HardwareDevice) SetKeyImage(key KeyID, img image.Image) error {
	return h.dev.SetKeyImage(streamdeck.KeyID(key), img)
}

// SetTouchStripImage sets the touch strip image.
func (h *HardwareDevice) SetTouchStripImage(img image.Image) error {
	return h.dev.SetTouchStripImage(img)
}

// ClearKey clears a key's image.
func (h *HardwareDevice) ClearKey(key KeyID) error {
	return h.dev.ClearKey(streamdeck.KeyID(key))
}

// ForEachKey calls the callback for each key.
func (h *HardwareDevice) ForEachKey(cb func(KeyID) error) error {
	return h.dev.ForEachKey(func(k streamdeck.KeyID) error {
		return cb(KeyID(k))
	})
}

// ForEachDial calls the callback for each dial.
func (h *HardwareDevice) ForEachDial(cb func(DialID) error) error {
	return h.dev.ForEachDial(func(d streamdeck.DialID) error {
		return cb(DialID(d))
	})
}

// hardwareKey wraps streamdeck.Key to implement the Key interface.
type hardwareKey struct {
	key *streamdeck.Key
}

func (k *hardwareKey) GetID() KeyID {
	return KeyID(k.key.GetID())
}

func (k *hardwareKey) WaitForRelease() time.Duration {
	return k.key.WaitForRelease()
}

// hardwareDial wraps streamdeck.Dial to implement the Dial interface.
type hardwareDial struct {
	dial *streamdeck.Dial
}

func (d *hardwareDial) GetID() DialID {
	return DialID(d.dial.GetID())
}

func (d *hardwareDial) WaitForRelease() time.Duration {
	return d.dial.WaitForRelease()
}

// AddKeyHandler adds a handler for a key press.
func (h *HardwareDevice) AddKeyHandler(key KeyID, fn KeyHandler) error {
	return h.dev.AddKeyHandler(streamdeck.KeyID(key), func(d *streamdeck.Device, k *streamdeck.Key) error {
		return fn(h, &hardwareKey{key: k})
	})
}

// AddDialRotateHandler adds a handler for dial rotation.
func (h *HardwareDevice) AddDialRotateHandler(dial DialID, fn DialRotateHandler) error {
	return h.dev.AddDialRotateHandler(streamdeck.DialID(dial), func(d *streamdeck.Device, di *streamdeck.Dial, delta int8) error {
		return fn(h, &hardwareDial{dial: di}, delta)
	})
}

// AddDialSwitchHandler adds a handler for dial press.
func (h *HardwareDevice) AddDialSwitchHandler(dial DialID, fn DialSwitchHandler) error {
	return h.dev.AddDialSwitchHandler(streamdeck.DialID(dial), func(d *streamdeck.Device, di *streamdeck.Dial) error {
		return fn(h, &hardwareDial{dial: di})
	})
}

// AddTouchStripTouchHandler adds a handler for touch strip touches.
func (h *HardwareDevice) AddTouchStripTouchHandler(fn TouchStripTouchHandler) error {
	return h.dev.AddTouchStripTouchHandler(func(d *streamdeck.Device, t streamdeck.TouchStripTouchType, p image.Point) error {
		return fn(h, TouchStripTouchType(t), p)
	})
}

// AddTouchStripSwipeHandler adds a handler for touch strip swipes.
func (h *HardwareDevice) AddTouchStripSwipeHandler(fn TouchStripSwipeHandler) error {
	return h.dev.AddTouchStripSwipeHandler(func(d *streamdeck.Device, origin, destination image.Point) error {
		return fn(h, origin, destination)
	})
}

// Listen starts the device event loop.
func (h *HardwareDevice) Listen(errCh chan error) error {
	return h.dev.Listen(errCh)
}

// Underlying returns the underlying streamdeck.Device for direct access when needed.
func (h *HardwareDevice) Underlying() *streamdeck.Device {
	return h.dev
}
