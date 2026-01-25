// Package module defines the interface for Stream Deck feature modules.
package module

import "image"

// KeyID identifies a physical key on the Stream Deck.
// Stream Deck Plus has 8 keys (Key1-Key8).
type KeyID uint8

const (
	Key1 KeyID = iota + 1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
)

// DialID identifies a rotary dial on the Stream Deck Plus.
// Stream Deck Plus has 4 dials (Dial1-Dial4).
type DialID uint8

const (
	Dial1 DialID = iota + 1
	Dial2
	Dial3
	Dial4
)

// Resources defines the hardware resources allocated to a module.
type Resources struct {
	// Keys assigned to this module (may be empty).
	Keys []KeyID

	// StripRect is the region of the touch strip allocated to this module.
	// A zero rect means no strip region is allocated.
	StripRect image.Rectangle

	// Dials assigned to this module (may be empty).
	Dials []DialID
}

// HasKeys returns true if this module has any keys allocated.
func (r Resources) HasKeys() bool {
	return len(r.Keys) > 0
}

// HasStrip returns true if this module has a touch strip region allocated.
func (r Resources) HasStrip() bool {
	return !r.StripRect.Empty()
}

// HasDials returns true if this module has any dials allocated.
func (r Resources) HasDials() bool {
	return len(r.Dials) > 0
}

// OwnsKey returns true if the given key is allocated to this module.
func (r Resources) OwnsKey(key KeyID) bool {
	for _, k := range r.Keys {
		if k == key {
			return true
		}
	}
	return false
}

// OwnsDial returns true if the given dial is allocated to this module.
func (r Resources) OwnsDial(dial DialID) bool {
	for _, d := range r.Dials {
		if d == dial {
			return true
		}
	}
	return false
}
