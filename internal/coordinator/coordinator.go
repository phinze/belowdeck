// Package coordinator manages module lifecycle and routes events to modules.
package coordinator

import (
	"context"
	"image"
	"image/draw"
	"log"
	"sync"
	"time"

	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/module"
)

// Coordinator manages the lifecycle of modules and routes events to them.
type Coordinator struct {
	device  device.Device
	modules []module.Module

	// Resource tracking
	moduleResources map[module.Module]module.Resources

	// Ownership maps for event routing
	keyOwners  map[module.KeyID]module.Module
	dialOwners map[module.DialID]module.Module

	// Track modules that failed to initialize
	failedModules map[module.Module]bool

	// Strip compositing
	stripRect image.Rectangle

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// State tracking
	mu sync.RWMutex

	// Overlay state tracking
	overlayWasActive bool
}

// New creates a new Coordinator for the given device.
func New(dev device.Device) *Coordinator {
	return &Coordinator{
		device:          dev,
		modules:         make([]module.Module, 0),
		moduleResources: make(map[module.Module]module.Resources),
		keyOwners:       make(map[module.KeyID]module.Module),
		dialOwners:      make(map[module.DialID]module.Module),
		failedModules:   make(map[module.Module]bool),
	}
}

// RegisterModule registers a module with its allocated resources.
// Must be called before Start.
func (c *Coordinator) RegisterModule(m module.Module, res module.Resources) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Store resources for this module
	c.moduleResources[m] = res

	// Build ownership maps
	for _, key := range res.Keys {
		c.keyOwners[key] = m
	}
	for _, dial := range res.Dials {
		c.dialOwners[dial] = m
	}

	// Track module
	c.modules = append(c.modules, m)

	return nil
}

// Start initializes all modules and begins the event/render loop.
func (c *Coordinator) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Get full strip rectangle for compositing
	if c.device.GetTouchStripSupported() {
		rect, err := c.device.GetTouchStripImageRectangle()
		if err == nil {
			c.stripRect = rect
		}
	}

	// Initialize all modules (continue on error, just skip failed modules)
	for _, m := range c.modules {
		res := c.resourcesForModule(m)
		if err := m.Init(c.ctx, res); err != nil {
			log.Printf("Module %s failed to initialize: %v (skipping)", m.ID(), err)
			c.failedModules[m] = true
		}
	}

	// Setup event handlers
	c.setupEventHandlers()

	// Start device listener
	listenErr := make(chan error, 1)
	go func() {
		err := c.device.Listen(nil) // errors logged to stderr
		if err != nil {
			listenErr <- err
		}
		close(listenErr)
	}()

	// Start render loop
	c.wg.Add(1)
	go c.renderLoop()

	// Wait for context cancellation or device disconnect
	select {
	case <-c.ctx.Done():
		return nil
	case err := <-listenErr:
		// Device disconnected or listener error
		return err
	}
}

// Stop gracefully shuts down all modules.
func (c *Coordinator) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Stop all modules
	for _, m := range c.modules {
		m.Stop()
	}

	c.wg.Wait()
	return nil
}

// resourcesForModule returns the stored resources for a module.
func (c *Coordinator) resourcesForModule(m module.Module) module.Resources {
	return c.moduleResources[m]
}

// getActiveOverlay returns the active overlay provider, if any.
func (c *Coordinator) getActiveOverlay() module.OverlayProvider {
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		if overlay, ok := m.(module.OverlayProvider); ok && overlay.IsOverlayActive() {
			return overlay
		}
	}
	return nil
}

// setupEventHandlers registers device event handlers that route to modules.
func (c *Coordinator) setupEventHandlers() {
	// Key handlers - register for ALL keys, not just owned ones
	allKeys := []module.KeyID{
		module.Key1, module.Key2, module.Key3, module.Key4,
		module.Key5, module.Key6, module.Key7, module.Key8,
	}

	for _, keyID := range allKeys {
		key := keyID
		owner := c.keyOwners[key] // may be nil for unowned keys
		c.device.AddKeyHandler(device.KeyID(key), func(d device.Device, k device.Key) error {
			// Check for active overlay first
			if overlay := c.getActiveOverlay(); overlay != nil {
				// Route to overlay handler
				event := module.KeyEvent{Pressed: true}
				if err := overlay.HandleOverlayKey(key, event); err != nil {
					return err
				}
				duration := k.WaitForRelease()
				event = module.KeyEvent{Pressed: false, Duration: duration}
				return overlay.HandleOverlayKey(key, event)
			}

			// No overlay - route to owner if exists
			if owner == nil || c.failedModules[owner] {
				return nil
			}
			// Create press event
			event := module.KeyEvent{Pressed: true}
			if err := owner.HandleKey(key, event); err != nil {
				return err
			}

			// Wait for release and create release event
			duration := k.WaitForRelease()
			event = module.KeyEvent{Pressed: false, Duration: duration}
			return owner.HandleKey(key, event)
		})
	}

	// Dial rotation handlers - register for ALL dials to support overlay
	allDials := []module.DialID{module.Dial1, module.Dial2, module.Dial3, module.Dial4}
	for _, dialID := range allDials {
		dial := dialID
		owner := c.dialOwners[dial] // may be nil for unowned dials
		c.device.AddDialRotateHandler(device.DialID(dial), func(d device.Device, di device.Dial, delta int8) error {
			event := module.DialEvent{
				Type:  module.DialRotate,
				Delta: delta,
			}
			// Check for active overlay first
			if overlay := c.getActiveOverlay(); overlay != nil {
				return overlay.HandleOverlayDial(dial, event)
			}
			// No overlay - route to owner if exists
			if owner == nil || c.failedModules[owner] {
				return nil
			}
			return owner.HandleDial(dial, event)
		})
	}

	// Dial press handlers - register for ALL dials to support overlay
	for _, dialID := range allDials {
		dial := dialID
		owner := c.dialOwners[dial] // may be nil for unowned dials
		c.device.AddDialSwitchHandler(device.DialID(dial), func(d device.Device, di device.Dial) error {
			// Check for active overlay first
			if overlay := c.getActiveOverlay(); overlay != nil {
				// Create press event
				event := module.DialEvent{Type: module.DialPress}
				if err := overlay.HandleOverlayDial(dial, event); err != nil {
					return err
				}
				// Wait for release and create release event
				duration := di.WaitForRelease()
				event = module.DialEvent{Type: module.DialRelease, Duration: duration}
				return overlay.HandleOverlayDial(dial, event)
			}
			// No overlay - route to owner if exists
			if owner == nil || c.failedModules[owner] {
				return nil
			}
			// Create press event
			event := module.DialEvent{Type: module.DialPress}
			if err := owner.HandleDial(dial, event); err != nil {
				return err
			}
			// Wait for release and create release event
			duration := di.WaitForRelease()
			event = module.DialEvent{Type: module.DialRelease, Duration: duration}
			return owner.HandleDial(dial, event)
		})
	}

	// Touch strip handler - route based on X coordinate
	if c.device.GetTouchStripSupported() {
		c.device.AddTouchStripTouchHandler(func(d device.Device, touchType device.TouchStripTouchType, point image.Point) error {
			event := module.TouchStripEventFromDeviceTap(touchType, point)
			// Check for active overlay first
			if overlay := c.getActiveOverlay(); overlay != nil {
				return overlay.HandleOverlayStripTouch(event)
			}
			return c.routeStripEvent(event)
		})

		c.device.AddTouchStripSwipeHandler(func(d device.Device, origin, dest image.Point) error {
			event := module.TouchStripEventFromSwipe(origin, dest)
			// Check for active overlay first
			if overlay := c.getActiveOverlay(); overlay != nil {
				return overlay.HandleOverlayStripTouch(event)
			}
			return c.routeStripEvent(event)
		})
	}
}

// routeStripEvent finds the owning module for a strip event and dispatches it.
func (c *Coordinator) routeStripEvent(event module.TouchStripEvent) error {
	// For now, route to first module that has a strip region
	// Future: check which module's strip rect contains the event point
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		res := c.resourcesForModule(m)
		if res.HasStrip() {
			return m.HandleStripTouch(event)
		}
	}
	return nil
}

// renderLoop runs the periodic render cycle.
func (c *Coordinator) renderLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Initial render
	c.renderKeys()
	c.renderStrip()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.renderKeys()
			c.renderStrip()
		}
	}
}

// renderKeys collects key images from all modules and applies them to the device.
func (c *Coordinator) renderKeys() {
	// Check for active overlays first
	overlayActive := false
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		if overlay, ok := m.(module.OverlayProvider); ok && overlay.IsOverlayActive() {
			overlayActive = true
			// Overlay takes over all keys
			keyImages := overlay.RenderOverlayKeys()
			for keyID, img := range keyImages {
				if img != nil {
					c.device.SetKeyImage(device.KeyID(keyID), img)
				}
			}
			c.overlayWasActive = true
			return
		}
	}

	// If overlay just became inactive, clear all keys first
	if c.overlayWasActive && !overlayActive {
		c.clearAllKeys()
		c.overlayWasActive = false
	}

	// Normal rendering
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		keyImages := m.RenderKeys()
		for keyID, img := range keyImages {
			if img != nil {
				c.device.SetKeyImage(device.KeyID(keyID), img)
			}
		}
	}
}

// renderStrip composites strip images from all modules and applies to the device.
func (c *Coordinator) renderStrip() {
	if c.stripRect.Empty() {
		return
	}

	// Check for active overlays first
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		if overlay, ok := m.(module.OverlayProvider); ok && overlay.IsOverlayActive() {
			// Overlay takes over the strip
			stripImg := overlay.RenderOverlayStrip()
			if stripImg != nil {
				c.device.SetTouchStripImage(stripImg)
			}
			return
		}
	}

	// Create composite strip image
	composite := image.NewRGBA(c.stripRect)

	// Collect and composite each module's strip output
	for _, m := range c.modules {
		if c.failedModules[m] {
			continue
		}
		res := c.resourcesForModule(m)
		if !res.HasStrip() {
			continue
		}

		stripImg := m.RenderStrip()
		if stripImg == nil {
			continue
		}

		// Draw module's strip at its allocated region
		// For now, we draw at 0,0 - in future, we'd use res.StripRect offset
		draw.Draw(composite, stripImg.Bounds(), stripImg, image.Point{}, draw.Over)
	}

	c.device.SetTouchStripImage(composite)
}

// Device returns the underlying device.
// Modules can use this to query device capabilities like key size.
func (c *Coordinator) Device() device.Device {
	return c.device
}

// clearAllKeys sets all keys to black.
func (c *Coordinator) clearAllKeys() {
	allKeys := []module.KeyID{
		module.Key1, module.Key2, module.Key3, module.Key4,
		module.Key5, module.Key6, module.Key7, module.Key8,
	}

	// Create a black image for clearing
	keyRect, err := c.device.GetKeyImageRectangle()
	if err != nil {
		return
	}
	blackImg := image.NewRGBA(keyRect)

	for _, keyID := range allKeys {
		c.device.SetKeyImage(device.KeyID(keyID), blackImg)
	}
}
