// Package device defines the abstraction layer for Stream Deck hardware.
package device

import (
	"image"
	"time"
)

// Device is the interface that abstracts Stream Deck hardware.
// Both the real hardware adapter and the emulator implement this interface.
type Device interface {
	// Lifecycle
	Open() error
	Close() error
	IsOpen() bool

	// Device info
	GetModelName() string
	GetKeyCount() byte
	GetDialCount() byte
	GetTouchStripSupported() bool
	GetKeyImageRectangle() (image.Rectangle, error)
	GetTouchStripImageRectangle() (image.Rectangle, error)

	// Display
	SetBrightness(perc byte) error
	SetKeyImage(key KeyID, img image.Image) error
	SetTouchStripImage(img image.Image) error
	ClearKey(key KeyID) error

	// Iteration
	ForEachKey(cb func(KeyID) error) error
	ForEachDial(cb func(DialID) error) error

	// Event handlers
	AddKeyHandler(key KeyID, fn KeyHandler) error
	AddDialRotateHandler(dial DialID, fn DialRotateHandler) error
	AddDialSwitchHandler(dial DialID, fn DialSwitchHandler) error
	AddTouchStripTouchHandler(fn TouchStripTouchHandler) error
	AddTouchStripSwipeHandler(fn TouchStripSwipeHandler) error

	// Event loop
	Listen(errCh chan error) error
}

// KeyID identifies a physical key on the Stream Deck.
type KeyID byte

// Key IDs for Stream Deck Plus (8 keys)
const (
	KEY_1 KeyID = iota + 1
	KEY_2
	KEY_3
	KEY_4
	KEY_5
	KEY_6
	KEY_7
	KEY_8
)

// DialID identifies a rotary dial on the Stream Deck Plus.
type DialID byte

// Dial IDs for Stream Deck Plus (4 dials)
const (
	DIAL_1 DialID = iota + 1
	DIAL_2
	DIAL_3
	DIAL_4
)

// TouchStripTouchType represents the type of touch on the strip.
type TouchStripTouchType byte

// Touch strip touch types
const (
	TOUCH_STRIP_TOUCH_TYPE_SHORT TouchStripTouchType = iota + 1
	TOUCH_STRIP_TOUCH_TYPE_LONG
)

// Key represents a physical key and provides methods for handlers.
type Key interface {
	GetID() KeyID
	WaitForRelease() time.Duration
}

// Dial represents a rotary dial and provides methods for handlers.
type Dial interface {
	GetID() DialID
	WaitForRelease() time.Duration
}

// Handler types - note these use the local Device interface
type (
	// KeyHandler is called when a key is pressed.
	KeyHandler func(d Device, k Key) error

	// DialSwitchHandler is called when a dial is pressed.
	DialSwitchHandler func(d Device, di Dial) error

	// DialRotateHandler is called when a dial is rotated.
	DialRotateHandler func(d Device, di Dial, delta int8) error

	// TouchStripTouchHandler is called when the touch strip is touched.
	TouchStripTouchHandler func(d Device, t TouchStripTouchType, p image.Point) error

	// TouchStripSwipeHandler is called when the touch strip is swiped.
	TouchStripSwipeHandler func(d Device, origin, destination image.Point) error
)
