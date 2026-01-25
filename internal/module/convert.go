package module

import (
	"image"

	"rafaelmartins.com/p/streamdeck"
)

// ToStreamdeck converts a module KeyID to the streamdeck library KeyID.
func (k KeyID) ToStreamdeck() streamdeck.KeyID {
	return streamdeck.KeyID(k)
}

// KeyIDFromStreamdeck converts a streamdeck library KeyID to a module KeyID.
func KeyIDFromStreamdeck(k streamdeck.KeyID) KeyID {
	return KeyID(k)
}

// ToStreamdeck converts a module DialID to the streamdeck library DialID.
func (d DialID) ToStreamdeck() streamdeck.DialID {
	return streamdeck.DialID(d)
}

// DialIDFromStreamdeck converts a streamdeck library DialID to a module DialID.
func DialIDFromStreamdeck(d streamdeck.DialID) DialID {
	return DialID(d)
}

// TouchStripEventFromTap creates a TouchStripEvent from a tap.
func TouchStripEventFromTap(touchType streamdeck.TouchStripTouchType, point image.Point) TouchStripEvent {
	var eventType TouchStripEventType
	switch touchType {
	case streamdeck.TOUCH_STRIP_TOUCH_TYPE_SHORT:
		eventType = TouchTap
	case streamdeck.TOUCH_STRIP_TOUCH_TYPE_LONG:
		eventType = TouchLongTap
	default:
		eventType = TouchTap
	}

	return TouchStripEvent{
		Type:  eventType,
		Point: point,
	}
}

// TouchStripEventFromSwipe creates a TouchStripEvent from a swipe gesture.
func TouchStripEventFromSwipe(origin, destination image.Point) TouchStripEvent {
	return TouchStripEvent{
		Type:       TouchSwipe,
		Point:      origin,
		SwipeStart: origin,
		SwipeEnd:   destination,
	}
}
