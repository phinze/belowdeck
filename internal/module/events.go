package module

import (
	"image"
	"time"
)

// DialEventType indicates the type of dial interaction.
type DialEventType uint8

const (
	// DialRotate indicates the dial was rotated.
	DialRotate DialEventType = iota + 1
	// DialPress indicates the dial was pressed down.
	DialPress
	// DialRelease indicates the dial was released.
	DialRelease
)

// DialEvent represents an interaction with a rotary dial.
type DialEvent struct {
	// Type indicates what kind of dial interaction occurred.
	Type DialEventType

	// Delta is the rotation amount (positive = clockwise, negative = counter-clockwise).
	// Only meaningful for DialRotate events.
	Delta int8

	// Duration is how long the dial was held before release.
	// Only meaningful for DialRelease events.
	Duration time.Duration
}

// KeyEvent represents an interaction with a physical key.
type KeyEvent struct {
	// Pressed is true when the key is pressed down, false when released.
	Pressed bool

	// Duration is how long the key was held before release.
	// Only meaningful when Pressed is false.
	Duration time.Duration
}

// TouchStripEventType indicates the type of touch strip interaction.
type TouchStripEventType uint8

const (
	// TouchTap indicates a short tap on the touch strip.
	TouchTap TouchStripEventType = iota + 1
	// TouchLongTap indicates a long press on the touch strip.
	TouchLongTap
	// TouchSwipe indicates a swipe gesture on the touch strip.
	TouchSwipe
)

// TouchStripEvent represents an interaction with the touch strip.
type TouchStripEvent struct {
	// Type indicates what kind of touch interaction occurred.
	Type TouchStripEventType

	// Point is the location of a tap or long tap.
	// For swipes, this is the same as SwipeStart.
	Point image.Point

	// SwipeStart is the starting point of a swipe gesture.
	// Only meaningful for TouchSwipe events.
	SwipeStart image.Point

	// SwipeEnd is the ending point of a swipe gesture.
	// Only meaningful for TouchSwipe events.
	SwipeEnd image.Point
}
