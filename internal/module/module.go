package module

import (
	"context"
	"image"
)

// Module defines the interface that all Stream Deck feature modules implement.
type Module interface {
	// ID returns a unique identifier for this module instance.
	ID() string

	// Init initializes the module with the given context and allocated resources.
	// The context should be used for cancellation and lifecycle management.
	Init(ctx context.Context, resources Resources) error

	// Stop gracefully shuts down the module, releasing any resources.
	Stop() error

	// RenderKeys returns images for each key allocated to this module.
	// Keys not in the returned map will not be updated.
	RenderKeys() map[KeyID]image.Image

	// RenderStrip returns an image for this module's touch strip region.
	// Returns nil if the module has no strip content to render.
	RenderStrip() image.Image

	// HandleKey processes a key event for the given key.
	HandleKey(id KeyID, event KeyEvent) error

	// HandleDial processes a dial event for the given dial.
	HandleDial(id DialID, event DialEvent) error

	// HandleStripTouch processes a touch event on the touch strip.
	HandleStripTouch(event TouchStripEvent) error
}
