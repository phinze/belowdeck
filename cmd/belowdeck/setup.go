package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/phinze/belowdeck/internal/config"
	"github.com/spf13/cobra"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup: write config and store secrets in Keychain",
	RunE:  runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("=== Belowdeck Setup ===")
	fmt.Println()

	// Load existing config as defaults
	existing, _ := config.Load()
	if existing == nil {
		existing = &config.Config{}
	}

	cfg := &config.Config{}

	// Weather config
	fmt.Println("-- Weather --")
	cfg.Weather.Lat = prompt(reader, "Weather latitude", existing.Weather.Lat)
	cfg.Weather.Lon = prompt(reader, "Weather longitude", existing.Weather.Lon)

	apiKey := promptSecret(reader, "OpenWeatherMap API key", existing.Weather.APIKey != "")
	if apiKey != "" {
		if err := config.SetKeychainSecret(config.KeyOpenWeatherMapAPIKey, apiKey); err != nil {
			return fmt.Errorf("storing API key in Keychain: %w", err)
		}
		fmt.Println("  -> Stored in Keychain")
	} else {
		fmt.Println("  -> Kept existing")
	}

	fmt.Println()

	// Home Assistant config
	fmt.Println("-- Home Assistant --")
	cfg.HomeAssistant.Server = prompt(reader, "Home Assistant server URL", existing.HomeAssistant.Server)
	cfg.HomeAssistant.RingLightEntity = prompt(reader, "Ring light entity ID", existing.HomeAssistant.RingLightEntity)
	cfg.HomeAssistant.OfficeLightEntity = prompt(reader, "Office light entity ID", existing.HomeAssistant.OfficeLightEntity)

	hassToken := promptSecret(reader, "Home Assistant token", existing.HomeAssistant.Token != "")
	if hassToken != "" {
		if err := config.SetKeychainSecret(config.KeyHASSToken, hassToken); err != nil {
			return fmt.Errorf("storing HA token in Keychain: %w", err)
		}
		fmt.Println("  -> Stored in Keychain")
	} else {
		fmt.Println("  -> Kept existing")
	}

	fmt.Println()

	// Write config file
	if err := config.WriteConfigFile(cfg); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	fmt.Printf("Config written to %s\n", config.DefaultConfigPath())
	fmt.Println("Setup complete!")
	return nil
}

// prompt asks for a value with an optional default.
func prompt(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("  %s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptSecret asks for a secret value. If one already exists, allows keeping it.
func promptSecret(reader *bufio.Reader, label string, hasExisting bool) string {
	if hasExisting {
		fmt.Printf("  %s [press Enter to keep existing]: ", label)
	} else {
		fmt.Printf("  %s: ", label)
	}
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}
