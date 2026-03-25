// Package homeassistant provides a Stream Deck module for Home Assistant control.
package homeassistant

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync"
	"time"

	"github.com/phinze/belowdeck/internal/config"
	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/module"
	"golang.org/x/image/font"
)

// Config holds the Home Assistant module configuration.
type Config struct {
	URL               string
	Token             string
	RingLightEntity   string
	OfficeLightEntity string
}

// Module implements the Home Assistant control module.
type Module struct {
	module.BaseModule

	device  device.Device
	appCfg  *config.Config
	config  Config
	client  *Client
	enabled bool

	// State
	mu               sync.RWMutex
	ringLightState   LightState
	officeLightState LightState

	// Fonts
	labelFace font.Face

	// Resources
	resources module.Resources
}

// New creates a new Home Assistant module.
func New(dev device.Device, appCfg *config.Config) *Module {
	return &Module{
		BaseModule: module.NewBaseModule("homeassistant"),
		device:     dev,
		appCfg:     appCfg,
	}
}

// ID returns the module identifier.
func (m *Module) ID() string {
	return "homeassistant"
}

// Init initializes the module.
func (m *Module) Init(ctx context.Context, res module.Resources) error {
	// Call base init
	if err := m.BaseModule.Init(ctx, res); err != nil {
		return err
	}

	m.resources = res

	// Load config (optional - module disabled if not configured)
	config, err := loadConfig(m.appCfg)
	if err != nil {
		log.Printf("Home Assistant module disabled: %v", err)
		m.enabled = false
		return nil
	}
	m.config = config
	m.enabled = true

	// Create API client
	m.client = NewClient(m.config.URL, m.config.Token)

	// Initialize fonts
	if err := m.initFonts(); err != nil {
		return err
	}

	// Start state polling
	go m.pollState(ctx)

	log.Printf("Home Assistant module initialized (url=%s)", m.config.URL)
	return nil
}

// pollState periodically fetches entity states from Home Assistant.
func (m *Module) pollState(ctx context.Context) {
	// Initial fetch
	m.fetchRingLightState(ctx)
	m.fetchOfficeLightState(ctx)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.fetchRingLightState(ctx)
			m.fetchOfficeLightState(ctx)
		}
	}
}

// fetchRingLightState fetches the current ring light state.
func (m *Module) fetchRingLightState(ctx context.Context) {
	state, err := m.client.GetLightState(ctx, m.config.RingLightEntity)
	if err != nil {
		log.Printf("Failed to fetch ring light state: %v", err)
		return
	}

	m.mu.Lock()
	m.ringLightState = state
	m.mu.Unlock()
}

// getRingLightState returns the current ring light state.
func (m *Module) getRingLightState() LightState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ringLightState
}

// fetchOfficeLightState fetches the current office light state.
func (m *Module) fetchOfficeLightState(ctx context.Context) {
	state, err := m.client.GetLightState(ctx, m.config.OfficeLightEntity)
	if err != nil {
		log.Printf("Failed to fetch office light state: %v", err)
		return
	}

	m.mu.Lock()
	m.officeLightState = state
	m.mu.Unlock()
}

// getOfficeLightState returns the current office light state.
func (m *Module) getOfficeLightState() LightState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.officeLightState
}

// Stop shuts down the module.
func (m *Module) Stop() error {
	return m.BaseModule.Stop()
}

// loadConfig builds module Config from the app-level config.
func loadConfig(appCfg *config.Config) (Config, error) {
	if appCfg == nil {
		return Config{}, fmt.Errorf("no configuration provided")
	}

	url := appCfg.HomeAssistant.Server
	if url == "" {
		return Config{}, fmt.Errorf("Home Assistant server not configured")
	}

	token := appCfg.HomeAssistant.Token
	if token == "" {
		return Config{}, fmt.Errorf("Home Assistant token not configured")
	}

	ringLightEntity := appCfg.HomeAssistant.RingLightEntity
	if ringLightEntity == "" {
		return Config{}, fmt.Errorf("Home Assistant ring light entity not configured")
	}

	officeLightEntity := appCfg.HomeAssistant.OfficeLightEntity
	if officeLightEntity == "" {
		officeLightEntity = "light.signe_gradient_floor_1"
	}

	return Config{
		URL:               url,
		Token:             token,
		RingLightEntity:   ringLightEntity,
		OfficeLightEntity: officeLightEntity,
	}, nil
}

// RenderKeys returns images for the module's keys.
func (m *Module) RenderKeys() map[module.KeyID]image.Image {
	if !m.enabled {
		return nil
	}

	keys := make(map[module.KeyID]image.Image)

	// Key 0: Office Time button
	if len(m.resources.Keys) > 0 {
		keys[m.resources.Keys[0]] = m.renderOfficeTimeButton()
	}

	// Key 1: Ring Light toggle
	if len(m.resources.Keys) > 1 {
		keys[m.resources.Keys[1]] = m.renderRingLightButton()
	}

	return keys
}

// RenderStrip returns the touch strip image.
func (m *Module) RenderStrip() image.Image {
	return nil
}

// HandleKey processes key events.
func (m *Module) HandleKey(id module.KeyID, event module.KeyEvent) error {
	if !m.enabled {
		return nil
	}

	// Only trigger on key press, not release
	if !event.Pressed {
		return nil
	}

	// Fire-and-forget: run HA calls in a goroutine so we never block the device listener.
	// Key 0: Office toggle button
	if len(m.resources.Keys) > 0 && id == m.resources.Keys[0] {
		go m.toggleOfficeMode()
		return nil
	}

	// Key 1: Ring Light toggle
	if len(m.resources.Keys) > 1 && id == m.resources.Keys[1] {
		go m.toggleRingLight()
		return nil
	}

	return nil
}

// toggleOfficeMode toggles between office time and quittin time based on office light state.
func (m *Module) toggleOfficeMode() {
	state := m.getOfficeLightState()

	if state.On {
		log.Println("Executing Quittin Time script...")
		err := m.client.CallService(m.Context(), "script", "turn_on", map[string]any{
			"entity_id": "script.quittin_time",
		})
		if err != nil {
			log.Printf("Failed to execute Quittin Time: %v", err)
			return
		}
		log.Println("Quittin Time script executed successfully")
	} else {
		log.Println("Executing Office Time script...")
		err := m.client.CallService(m.Context(), "script", "turn_on", map[string]any{
			"entity_id": "script.office_time",
		})
		if err != nil {
			log.Printf("Failed to execute Office Time: %v", err)
			return
		}
		log.Println("Office Time script executed successfully")
	}
}

// toggleRingLight toggles the ring light on/off.
func (m *Module) toggleRingLight() {
	log.Println("Toggling ring light...")

	err := m.client.CallService(m.Context(), "light", "toggle", map[string]any{
		"entity_id": m.config.RingLightEntity,
	})
	if err != nil {
		log.Printf("Failed to toggle ring light: %v", err)
		return
	}

	log.Println("Ring light toggled")
}

// adjustRingLightBrightness adjusts the ring light brightness by a delta.
func (m *Module) adjustRingLightBrightness(delta int8) {
	// Each dial tick adjusts brightness by ~10% (25 out of 255)
	step := int(delta) * 25

	log.Printf("Adjusting ring light brightness by %d", step)

	err := m.client.CallService(m.Context(), "light", "turn_on", map[string]any{
		"entity_id":       m.config.RingLightEntity,
		"brightness_step": step,
	})
	if err != nil {
		log.Printf("Failed to adjust ring light brightness: %v", err)
	}
}

// HandleDial processes dial events.
func (m *Module) HandleDial(id module.DialID, event module.DialEvent) error {
	if !m.enabled {
		return nil
	}

	// Only handle rotation events
	if event.Type != module.DialRotate {
		return nil
	}

	// Dial 0: Ring Light brightness (fire-and-forget)
	if len(m.resources.Dials) > 0 && id == m.resources.Dials[0] {
		go m.adjustRingLightBrightness(event.Delta)
		return nil
	}

	return nil
}

// HandleStripTouch processes touch strip events.
func (m *Module) HandleStripTouch(event module.TouchStripEvent) error {
	return nil
}
