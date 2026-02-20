// Package weather provides a Stream Deck module for weather display.
package weather

import (
	"context"
	"fmt"
	"image"
	"log"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/phinze/belowdeck/internal/config"
	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/module"
	"golang.org/x/image/font"
)

// Config holds the weather module configuration.
type Config struct {
	APIKey string
	Lat    float64
	Lon    float64
}

// Module implements the weather display module.
type Module struct {
	module.BaseModule

	device  device.Device
	appCfg  *config.Config
	config  Config

	// State
	state *weatherState
	mu    sync.RWMutex

	// Fonts
	tempSmallFace font.Face
	conditionFace font.Face

	// Cancel function for polling
	pollCancel context.CancelFunc
}

// weatherState holds the current weather data.
type weatherState struct {
	sync.RWMutex
	Current   CurrentWeather
	Daily     DailyForecast
	Precip    PrecipForecast
	LastFetch time.Time
}

func newWeatherState() *weatherState {
	return &weatherState{}
}

func (s *weatherState) get() (CurrentWeather, DailyForecast, PrecipForecast) {
	s.RLock()
	defer s.RUnlock()
	return s.Current, s.Daily, s.Precip
}

func (s *weatherState) update(current CurrentWeather, daily DailyForecast, precip PrecipForecast) {
	s.Lock()
	defer s.Unlock()
	s.Current = current
	s.Daily = daily
	s.Precip = precip
	s.LastFetch = time.Now()
}

// New creates a new Weather module.
func New(dev device.Device, appCfg *config.Config) *Module {
	return &Module{
		BaseModule: module.NewBaseModule("weather"),
		device:     dev,
		appCfg:     appCfg,
		state:      newWeatherState(),
	}
}

// ID returns the module identifier.
func (m *Module) ID() string {
	return "weather"
}

// Init initializes the module.
func (m *Module) Init(ctx context.Context, res module.Resources) error {
	// Call base init
	if err := m.BaseModule.Init(ctx, res); err != nil {
		return err
	}

	// Load config
	config, err := loadConfig(m.appCfg)
	if err != nil {
		return err
	}
	m.config = config

	// Initialize fonts
	if err := m.initFonts(); err != nil {
		return err
	}

	// Start polling in background
	pollCtx, cancel := context.WithCancel(ctx)
	m.pollCancel = cancel
	go m.pollWeather(pollCtx)

	log.Printf("Weather module initialized (lat=%.4f, lon=%.4f)", m.config.Lat, m.config.Lon)
	return nil
}

// Stop shuts down the module.
func (m *Module) Stop() error {
	if m.pollCancel != nil {
		m.pollCancel()
	}
	return m.BaseModule.Stop()
}

// loadConfig builds module Config from the app-level config.
func loadConfig(appCfg *config.Config) (Config, error) {
	if appCfg == nil {
		return Config{}, fmt.Errorf("no configuration provided")
	}

	apiKey := appCfg.Weather.APIKey
	if apiKey == "" {
		return Config{}, fmt.Errorf("OpenWeatherMap API key not configured")
	}

	if appCfg.Weather.Lat == "" || appCfg.Weather.Lon == "" {
		return Config{}, fmt.Errorf("weather lat/lon not configured")
	}

	lat, err := strconv.ParseFloat(appCfg.Weather.Lat, 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid weather lat: %w", err)
	}

	lon, err := strconv.ParseFloat(appCfg.Weather.Lon, 64)
	if err != nil {
		return Config{}, fmt.Errorf("invalid weather lon: %w", err)
	}

	return Config{
		APIKey: apiKey,
		Lat:    lat,
		Lon:    lon,
	}, nil
}

// pollWeather fetches weather data periodically.
func (m *Module) pollWeather(ctx context.Context) {
	// Fetch immediately on start
	m.fetchWeather(ctx)

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.fetchWeather(ctx)
		}
	}
}

// fetchWeather fetches current weather from the API.
func (m *Module) fetchWeather(ctx context.Context) {
	current, daily, precip, err := fetchOneCall(ctx, m.config.APIKey, m.config.Lat, m.config.Lon)
	if err != nil {
		log.Printf("Weather fetch error: %v", err)
		return
	}

	m.state.update(current, daily, precip)
	precipInfo := ""
	if precip.Description != "" {
		precipInfo = " | " + precip.Description
	}
	log.Printf("Weather updated: %.0f°F (feels %.0f°F) %s (H:%.0f° L:%.0f°)%s",
		current.Temp, current.FeelsLike, current.Description, daily.TempMax, daily.TempMin, precipInfo)
}

// RenderKeys returns images for the module's keys.
func (m *Module) RenderKeys() map[module.KeyID]image.Image {
	// Weather module doesn't use keys
	return nil
}

// RenderStrip returns the touch strip image.
func (m *Module) RenderStrip() image.Image {
	if !m.device.GetTouchStripSupported() {
		return nil
	}

	rect, err := m.device.GetTouchStripImageRectangle()
	if err != nil {
		return nil
	}

	current, daily, precip := m.state.get()
	return m.renderStrip(rect, current, daily, precip)
}

// HandleKey processes key events.
func (m *Module) HandleKey(id module.KeyID, event module.KeyEvent) error {
	// Weather module doesn't use keys
	return nil
}

// HandleDial processes dial events.
func (m *Module) HandleDial(id module.DialID, event module.DialEvent) error {
	// Weather module doesn't use dials
	return nil
}

// HandleStripTouch processes touch strip events.
func (m *Module) HandleStripTouch(event module.TouchStripEvent) error {
	if event.Type != module.TouchTap {
		return nil
	}

	log.Println("Strip tap: opening Weather")
	go exec.Command("open", "-a", "Weather").Run()
	return nil
}
