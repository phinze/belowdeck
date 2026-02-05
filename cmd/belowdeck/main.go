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

	"github.com/phinze/belowdeck/internal/coordinator"
	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/module"
	"github.com/phinze/belowdeck/internal/modules/github"
	"github.com/phinze/belowdeck/internal/modules/homeassistant"
	"github.com/phinze/belowdeck/internal/modules/nowplaying"
	"github.com/phinze/belowdeck/internal/modules/weather"
	"github.com/prashantgupta24/mac-sleep-notifier/notifier"
	"rafaelmartins.com/p/streamdeck"
)

func main() {
	log.Println("=== Stream Deck Daemon ===")
	log.Println("Press Ctrl+C to exit")

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

	// Start sleep/wake notifier and run device loop
	sleepCh := notifier.GetInstance().Start()
	wakeCh := make(chan struct{}, 1)
	go func() {
		for activity := range sleepCh {
			if activity.Type == notifier.Awake {
				log.Println("System wake detected")
				select {
				case wakeCh <- struct{}{}:
				default:
				}
			}
		}
	}()

	// Main device loop - wait for device, run, repeat on disconnect
	for {
		dev := waitForHardwareDevice(ctx, wakeCh)
		if dev == nil {
			// Context cancelled
			break
		}

		// Check context before starting - avoid race where device connects after shutdown requested
		select {
		case <-ctx.Done():
			log.Println("Exiting...")
			dev.Close()
			return
		default:
		}

		// Drain any stale wake signals that accumulated while waiting for device.
		// Without this, a wake signal from before device enumeration would
		// immediately trigger a teardown in runWithDevice.
	drainWake:
		for {
			select {
			case <-wakeCh:
				log.Println("Draining stale wake signal")
			default:
				break drainWake
			}
		}

		// Brief stabilization delay - USB device enumeration may not be complete
		// even after GetDevice succeeds. Give the device a moment to fully initialize.
		time.Sleep(500 * time.Millisecond)

		runWithDevice(ctx, dev, wakeCh)

		// Check if we should exit or wait for reconnect
		select {
		case <-ctx.Done():
			log.Println("Exiting...")
			return
		default:
			log.Println("Waiting for device reconnect...")
		}
	}
}

// tryGetDeviceWithTimeout attempts to get and open a Stream Deck device with a timeout.
// Returns the device if successful, nil otherwise. The timeout prevents blocking indefinitely
// when the USB subsystem is in a bad state.
func tryGetDeviceWithTimeout(timeout time.Duration) *streamdeck.Device {
	type result struct {
		dev *streamdeck.Device
		err error
	}
	ch := make(chan result, 1)

	go func() {
		dev, err := streamdeck.GetDevice("")
		if err != nil {
			ch <- result{nil, err}
			return
		}
		if err := dev.Open(); err != nil {
			ch <- result{nil, err}
			return
		}
		ch <- result{dev, nil}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil
		}
		return r.dev
	case <-time.After(timeout):
		log.Println("Device detection timed out")
		return nil
	}
}

// waitForHardwareDevice polls for a Stream Deck device until one is available.
// Uses polling since macOS doesn't have a simple USB hotplug event API.
// Wake signals trigger immediate retry instead of waiting for the poll interval.
func waitForHardwareDevice(ctx context.Context, wakeCh <-chan struct{}) device.Device {
	const deviceTimeout = 5 * time.Second

	// First, try to get an already-connected device
	if dev := tryGetDeviceWithTimeout(deviceTimeout); dev != nil {
		return device.NewHardware(dev)
	}

	log.Println("Waiting for device...")

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-wakeCh:
			// After wake, USB devices may take several seconds to enumerate.
			// Retry multiple times with short delays instead of just checking once.
			log.Println("Wake signal received, probing for device...")
			for i := 0; i < 10; i++ {
				if dev := tryGetDeviceWithTimeout(deviceTimeout); dev != nil {
					log.Println("Device connected!")
					return device.NewHardware(dev)
				}
				// Context-aware sleep
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(500 * time.Millisecond):
				}
			}
			log.Println("Device not found after wake, resuming polling...")
		case <-time.After(2 * time.Second):
		}

		if dev := tryGetDeviceWithTimeout(deviceTimeout); dev != nil {
			log.Println("Device connected!")
			return device.NewHardware(dev)
		}
	}
}

// runWithDevice runs the coordinator with the given device until disconnect, wake, or context cancel.
func runWithDevice(ctx context.Context, dev device.Device, wakeCh <-chan struct{}) {
	log.Printf("Connected to: %s", dev.GetModelName())

	// Set brightness and clear keys
	dev.SetBrightness(80)
	dev.ForEachKey(func(key device.KeyID) error {
		return dev.ClearKey(key)
	})

	// Create coordinator and modules fresh for each connection
	coord := coordinator.New(dev)

	np := nowplaying.New(dev)
	coord.RegisterModule(np, module.Resources{
		Keys:      []module.KeyID{module.Key5, module.Key6},
		StripRect: image.Rect(0, 0, 400, 100),
		Dials:     []module.DialID{module.Dial1, module.Dial2},
	})

	w := weather.New(dev)
	coord.RegisterModule(w, module.Resources{
		StripRect: image.Rect(400, 0, 800, 100),
	})

	ha := homeassistant.New(dev)
	coord.RegisterModule(ha, module.Resources{
		Keys:  []module.KeyID{module.Key1, module.Key2},
		Dials: []module.DialID{module.Dial4},
	})

	gh := github.New(dev)
	coord.RegisterModule(gh, module.Resources{
		Keys: []module.KeyID{module.Key3, module.Key4},
	})

	// Run coordinator with a child context so we can stop it independently
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- coord.Start(runCtx)
	}()

	log.Println("Ready! Media on left, weather on right")

	// Wait for parent context cancel, device error, or system wake
	select {
	case <-ctx.Done():
		log.Println("Shutting down...")
	case err := <-errChan:
		if err != nil {
			log.Printf("Device disconnected: %v", err)
		}
	case <-wakeCh:
		log.Println("Reconnecting device after wake...")
	}

	// Stop coordinator with timeout
	runCancel()

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

	// Brief delay to let any pending USB I/O callbacks complete.
	// The usbhid library doesn't cancel ongoing I/O on close, so callbacks
	// can fire after close with stale context pointers causing crashes.
	time.Sleep(200 * time.Millisecond)

	// Close device - need to wait for this on wake to avoid race condition
	// where we try to reopen before close completes
	closeDone := make(chan struct{})
	go func() {
		dev.Close()
		close(closeDone)
	}()

	// If parent context is cancelled (shutdown signal), force exit
	// since device.Close() may block indefinitely
	select {
	case <-ctx.Done():
		log.Println("Exiting...")
		os.Exit(0)
	case <-closeDone:
		// Device closed cleanly
	case <-time.After(3 * time.Second):
		// Device close timed out - on wake, give it a bit more time
		// then proceed anyway (might need to wait for device to reappear)
		log.Println("Device close timed out")
	}
}
