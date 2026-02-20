// Package config provides configuration loading from YAML files, macOS Keychain,
// and environment variables. Environment variables take precedence for dev flexibility.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
	"gopkg.in/yaml.v3"
)

const (
	// KeychainService is the macOS Keychain service name for belowdeck secrets.
	KeychainService = "belowdeck"

	// Keychain account names for each secret.
	KeyOpenWeatherMapAPIKey = "openweathermap-api-key"
	KeyHASSToken            = "hass-token"
)

// Config holds the full application configuration, assembled from YAML + Keychain + env.
type Config struct {
	Weather      WeatherConfig      `yaml:"weather"`
	HomeAssistant HomeAssistantConfig `yaml:"homeassistant"`
}

// WeatherConfig holds weather module configuration.
type WeatherConfig struct {
	Lat    string `yaml:"lat"`
	Lon    string `yaml:"lon"`
	APIKey string `yaml:"-"` // secret, not in YAML
}

// HomeAssistantConfig holds Home Assistant module configuration.
type HomeAssistantConfig struct {
	Server            string `yaml:"server"`
	RingLightEntity   string `yaml:"ring_light_entity"`
	OfficeLightEntity string `yaml:"office_light_entity"`
	Token             string `yaml:"-"` // secret, not in YAML
}

// DefaultConfigDir returns the default config directory path.
func DefaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "belowdeck")
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	// Allow override via environment variable (used by nix-generated config)
	if p := os.Getenv("BELOWDECK_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(DefaultConfigDir(), "config.yaml")
}

// Load assembles configuration from YAML file + Keychain + environment variables.
// Environment variables always take precedence. Returns a usable Config even if
// some sources are missing (modules handle their own "not configured" state).
func Load() (*Config, error) {
	cfg := &Config{}

	// 1. Try to load YAML config file
	configPath := DefaultConfigPath()
	if data, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	// 2. Layer in Keychain secrets (ignore errors — Keychain may not be populated)
	if key, err := keyring.Get(KeychainService, KeyOpenWeatherMapAPIKey); err == nil {
		cfg.Weather.APIKey = key
	}
	if token, err := keyring.Get(KeychainService, KeyHASSToken); err == nil {
		cfg.HomeAssistant.Token = token
	}

	// 3. Environment variables override everything
	if v := os.Getenv("OPENWEATHERMAP_API_KEY"); v != "" {
		cfg.Weather.APIKey = v
	}
	if v := os.Getenv("WEATHER_LAT"); v != "" {
		cfg.Weather.Lat = v
	}
	if v := os.Getenv("WEATHER_LON"); v != "" {
		cfg.Weather.Lon = v
	}
	if v := os.Getenv("HASS_SERVER"); v != "" {
		cfg.HomeAssistant.Server = v
	}
	if v := os.Getenv("HASS_TOKEN"); v != "" {
		cfg.HomeAssistant.Token = v
	}
	if v := os.Getenv("HASS_RING_LIGHT_ENTITY"); v != "" {
		cfg.HomeAssistant.RingLightEntity = v
	}
	if v := os.Getenv("HASS_OFFICE_LIGHT_ENTITY"); v != "" {
		cfg.HomeAssistant.OfficeLightEntity = v
	}

	return cfg, nil
}

// WriteConfigFile writes the non-secret portion of config to the YAML file.
func WriteConfigFile(cfg *Config) error {
	dir := filepath.Dir(DefaultConfigPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(DefaultConfigPath(), data, 0o644)
}

// SetKeychainSecret stores a secret in the macOS Keychain.
func SetKeychainSecret(account, value string) error {
	// Delete first to avoid "already exists" errors on update
	_ = keyring.Delete(KeychainService, account)
	return keyring.Set(KeychainService, account, value)
}

// GetKeychainSecret retrieves a secret from the macOS Keychain.
func GetKeychainSecret(account string) (string, error) {
	return keyring.Get(KeychainService, account)
}
