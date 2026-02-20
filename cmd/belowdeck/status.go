package main

import (
	"fmt"
	"os"

	"github.com/phinze/belowdeck/internal/config"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check config, secrets, and device health",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Belowdeck Status ===")
	fmt.Println()

	allOK := true

	// Config file
	configPath := config.DefaultConfigPath()
	fmt.Printf("Config file: %s\n", configPath)
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("  Status: found")
	} else {
		fmt.Println("  Status: NOT FOUND")
		allOK = false
	}

	// Load config to check values
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("  Load error: %v\n", err)
		allOK = false
	}
	fmt.Println()

	// Weather
	fmt.Println("Weather:")
	if cfg != nil && cfg.Weather.Lat != "" && cfg.Weather.Lon != "" {
		fmt.Printf("  Location: %s, %s\n", cfg.Weather.Lat, cfg.Weather.Lon)
	} else {
		fmt.Println("  Location: NOT SET")
		allOK = false
	}

	if _, err := config.GetKeychainSecret(config.KeyOpenWeatherMapAPIKey); err == nil {
		fmt.Println("  API Key (Keychain): set")
	} else if cfg != nil && cfg.Weather.APIKey != "" {
		fmt.Println("  API Key (env): set")
	} else {
		fmt.Println("  API Key: NOT SET")
		allOK = false
	}
	fmt.Println()

	// Home Assistant
	fmt.Println("Home Assistant:")
	if cfg != nil && cfg.HomeAssistant.Server != "" {
		fmt.Printf("  Server: %s\n", cfg.HomeAssistant.Server)
	} else {
		fmt.Println("  Server: NOT SET")
		allOK = false
	}

	if cfg != nil && cfg.HomeAssistant.RingLightEntity != "" {
		fmt.Printf("  Ring light: %s\n", cfg.HomeAssistant.RingLightEntity)
	} else {
		fmt.Println("  Ring light: NOT SET")
		allOK = false
	}

	if cfg != nil && cfg.HomeAssistant.OfficeLightEntity != "" {
		fmt.Printf("  Office light: %s\n", cfg.HomeAssistant.OfficeLightEntity)
	} else {
		fmt.Println("  Office light: NOT SET")
	}

	if _, err := config.GetKeychainSecret(config.KeyHASSToken); err == nil {
		fmt.Println("  Token (Keychain): set")
	} else if cfg != nil && cfg.HomeAssistant.Token != "" {
		fmt.Println("  Token (env): set")
	} else {
		fmt.Println("  Token: NOT SET")
		allOK = false
	}
	fmt.Println()

	// Device check (quick USB probe)
	fmt.Println("Stream Deck:")
	dev := tryGetDeviceWithTimeout(2_000_000_000) // 2s
	if dev != nil {
		fmt.Println("  Device: CONNECTED")
		dev.Close()
	} else {
		fmt.Println("  Device: not detected")
	}
	fmt.Println()

	if allOK {
		fmt.Println("All checks passed.")
	} else {
		fmt.Println("Some checks failed. Run 'belowdeck setup' to configure.")
	}

	return nil
}
