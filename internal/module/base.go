package module

import (
	"context"
	"image"
)

// BaseModule provides default no-op implementations of the Module interface.
// Embed this in module implementations to only override the methods needed.
type BaseModule struct {
	id        string
	resources Resources
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewBaseModule creates a BaseModule with the given ID.
func NewBaseModule(id string) BaseModule {
	return BaseModule{id: id}
}

// ID returns the module's identifier.
func (b *BaseModule) ID() string {
	return b.id
}

// Init stores the context and resources for the module.
// Override this to perform module-specific initialization, but call the base
// implementation to ensure resources and context are properly stored.
func (b *BaseModule) Init(ctx context.Context, resources Resources) error {
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.resources = resources
	return nil
}

// Stop cancels the module's context.
// Override this to perform module-specific cleanup, but call the base
// implementation to ensure the context is cancelled.
func (b *BaseModule) Stop() error {
	if b.cancel != nil {
		b.cancel()
	}
	return nil
}

// RenderKeys returns nil by default (no key updates).
func (b *BaseModule) RenderKeys() map[KeyID]image.Image {
	return nil
}

// RenderStrip returns nil by default (no strip updates).
func (b *BaseModule) RenderStrip() image.Image {
	return nil
}

// HandleKey is a no-op by default.
func (b *BaseModule) HandleKey(id KeyID, event KeyEvent) error {
	return nil
}

// HandleDial is a no-op by default.
func (b *BaseModule) HandleDial(id DialID, event DialEvent) error {
	return nil
}

// HandleStripTouch is a no-op by default.
func (b *BaseModule) HandleStripTouch(event TouchStripEvent) error {
	return nil
}

// Resources returns the allocated resources for this module.
func (b *BaseModule) Resources() Resources {
	return b.resources
}

// Context returns the module's context.
func (b *BaseModule) Context() context.Context {
	return b.ctx
}
