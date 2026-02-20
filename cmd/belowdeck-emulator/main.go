package main

import (
	"context"
	"image"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/phinze/belowdeck/internal/config"
	"github.com/phinze/belowdeck/internal/coordinator"
	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/device/emulator"
	"github.com/phinze/belowdeck/internal/module"
	"github.com/phinze/belowdeck/internal/modules/github"
	"github.com/phinze/belowdeck/internal/modules/homeassistant"
	"github.com/phinze/belowdeck/internal/modules/nowplaying"
	"github.com/phinze/belowdeck/internal/modules/weather"
)

func main() {
	log.Println("=== Stream Deck Emulator ===")
	log.Println("Close window or press Ctrl+C to exit")

	// Check if media-control is available
	if _, err := exec.LookPath("media-control"); err != nil {
		log.Fatal("media-control not found. Install with: brew tap ungive/media-control && brew install media-control")
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\nReceived shutdown signal")
		cancel()
	}()

	emu := emulator.New()
	if err := emu.Open(); err != nil {
		log.Fatalf("Failed to open emulator: %v", err)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: config load: %v", err)
	}

	// Start coordinator in background goroutine
	go runWithDevice(ctx, cfg, emu)

	// Run GUI on main thread (required for macOS)
	if err := emu.RunGUI(); err != nil {
		log.Printf("Emulator GUI error: %v", err)
	}
}

// runWithDevice runs the coordinator with the given device until context cancel.
func runWithDevice(ctx context.Context, cfg *config.Config, dev device.Device) {
	log.Printf("Connected to: %s", dev.GetModelName())

	// Set brightness and clear keys
	dev.SetBrightness(80)
	dev.ForEachKey(func(key device.KeyID) error {
		return dev.ClearKey(key)
	})

	// Create coordinator and modules
	coord := coordinator.New(dev)

	np := nowplaying.New(dev)
	coord.RegisterModule(np, module.Resources{
		Keys:      []module.KeyID{module.Key5, module.Key6},
		StripRect: image.Rect(0, 0, 400, 100),
		Dials:     []module.DialID{module.Dial1, module.Dial2},
	})

	w := weather.New(dev, cfg)
	coord.RegisterModule(w, module.Resources{
		StripRect: image.Rect(400, 0, 800, 100),
	})

	ha := homeassistant.New(dev, cfg)
	coord.RegisterModule(ha, module.Resources{
		Keys:  []module.KeyID{module.Key1, module.Key2},
		Dials: []module.DialID{module.Dial4},
	})

	gh := github.New(dev)
	coord.RegisterModule(gh, module.Resources{
		Keys: []module.KeyID{module.Key3, module.Key4},
	})

	// Run coordinator
	errChan := make(chan error, 1)
	go func() {
		errChan <- coord.Start(ctx)
	}()

	log.Println("Ready! Media on left, weather on right")

	// Wait for context cancel or error
	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errChan:
		if err != nil {
			log.Printf("Coordinator error: %v", err)
		}
	}

	// Stop coordinator with timeout
	done := make(chan struct{})
	go func() {
		coord.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		log.Println("Cleanup timed out")
	}

	dev.Close()
}
