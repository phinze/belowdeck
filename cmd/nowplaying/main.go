package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/image/colornames"
	"golang.org/x/image/draw"
	"rafaelmartins.com/p/streamdeck"
)

// NowPlaying represents the media-control JSON output
type NowPlaying struct {
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Duration    float64 `json:"duration"`
	ElapsedTime float64 `json:"elapsedTime"`
	Playing     bool    `json:"playing"`
	ArtworkData string  `json:"artworkData"`
	ArtworkMime string  `json:"artworkMimeType"`
}

// Layout:
// [1:Prev] [2:Play] [3:Art] [4:Art]
// [5:Next] [6:Info] [7:Art] [8:Art]

var (
	keyPrev = streamdeck.KEY_1
	keyPlay = streamdeck.KEY_2
	keyNext = streamdeck.KEY_5
	keyInfo = streamdeck.KEY_6

	// Album art keys (2x2 grid on right side)
	artKeys = []streamdeck.KeyID{
		streamdeck.KEY_3, streamdeck.KEY_4, // top row
		streamdeck.KEY_7, streamdeck.KEY_8, // bottom row
	}
)

func main() {
	log.Println("=== Stream Deck Now Playing ===")
	log.Println("Press Ctrl+C to exit")

	// Check if media-control is available
	if _, err := exec.LookPath("media-control"); err != nil {
		log.Fatal("media-control not found. Install with: brew tap ungive/media-control && brew install media-control")
	}

	// Get Stream Deck
	device, err := streamdeck.GetDevice("")
	if err != nil {
		log.Fatalf("Failed to get Stream Deck: %v", err)
	}

	if err := device.Open(); err != nil {
		log.Fatalf("Failed to open device: %v", err)
	}
	defer device.Close()

	log.Printf("Connected to: %s", device.GetModelName())

	// Set brightness
	device.SetBrightness(80)

	// Clear all keys first
	device.ForEachKey(func(key streamdeck.KeyID) error {
		return device.ClearKey(key)
	})

	// Draw control icons
	drawControlIcons(device)

	// Setup key handlers
	setupKeyControls(device)

	// Setup dial controls
	setupDialControls(device)

	// Context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start listening for device events
	errChan := make(chan error, 1)
	go func() {
		if err := device.Listen(errChan); err != nil {
			errChan <- err
		}
	}()

	// Poll for now playing info
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastArtwork string
	var lastPlaying bool

	// Initial update
	updateDisplay(device, &lastArtwork, &lastPlaying)

	log.Println("Ready! Controls on left, album art on right")

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigChan:
			log.Println("\nShutting down...")
			cancel()
			return
		case err := <-errChan:
			if err != nil {
				log.Printf("Device error: %v", err)
			}
		case <-ticker.C:
			updateDisplay(device, &lastArtwork, &lastPlaying)
		}
	}
}

func getNowPlaying() (*NowPlaying, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "media-control", "get")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("media-control failed: %w", err)
	}

	var np NowPlaying
	if err := json.Unmarshal(output, &np); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &np, nil
}

func updateDisplay(device *streamdeck.Device, lastArtwork *string, lastPlaying *bool) {
	np, err := getNowPlaying()
	if err != nil {
		log.Printf("Failed to get now playing: %v", err)
		return
	}

	// Update artwork if changed
	if np.ArtworkData != "" && np.ArtworkData != *lastArtwork {
		*lastArtwork = np.ArtworkData
		updateArtwork(device, np.ArtworkData)
	}

	// Update play/pause icon if state changed
	if np.Playing != *lastPlaying {
		*lastPlaying = np.Playing
		drawPlayPauseIcon(device, np.Playing)
	}

	// Update touch strip with progress bar
	updateTouchStrip(device, np)
}

func updateArtwork(device *streamdeck.Device, artworkBase64 string) {
	imgData, err := base64.StdEncoding.DecodeString(artworkBase64)
	if err != nil {
		log.Printf("Failed to decode artwork: %v", err)
		return
	}

	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		log.Printf("Failed to decode image: %v", err)
		return
	}

	keyRect, err := device.GetKeyImageRectangle()
	if err != nil {
		log.Printf("Failed to get key size: %v", err)
		return
	}

	keyW := keyRect.Dx()
	keyH := keyRect.Dy()

	// Scale album art to 2x2 key size (square)
	totalSize := keyW * 2
	scaled := scaleImageSquare(img, totalSize)

	// Split into 4 tiles for the 2x2 grid
	// artKeys order: [3, 4, 7, 8] which maps to:
	// [0,0] [1,0]  ->  KEY_3, KEY_4
	// [0,1] [1,1]  ->  KEY_7, KEY_8
	positions := []image.Point{
		{0, 0}, {1, 0}, // top row: KEY_3, KEY_4
		{0, 1}, {1, 1}, // bottom row: KEY_7, KEY_8
	}

	for i, key := range artKeys {
		pos := positions[i]
		tile := image.NewRGBA(image.Rect(0, 0, keyW, keyH))
		srcRect := image.Rect(pos.X*keyW, pos.Y*keyH, (pos.X+1)*keyW, (pos.Y+1)*keyH)
		draw.Draw(tile, tile.Bounds(), scaled, srcRect.Min, draw.Src)

		if err := device.SetKeyImage(key, tile); err != nil {
			log.Printf("Failed to set key %d image: %v", key, err)
		}
	}

	log.Println("Updated album artwork")
}

func scaleImageSquare(src image.Image, size int) image.Image {
	// Scale to square, cropping if needed
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	// Crop to square from center
	var cropRect image.Rectangle
	if srcW > srcH {
		offset := (srcW - srcH) / 2
		cropRect = image.Rect(offset, 0, offset+srcH, srcH)
	} else {
		offset := (srcH - srcW) / 2
		cropRect = image.Rect(0, offset, srcW, offset+srcW)
	}

	// Scale the cropped square to target size
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
	return dst
}

func updateTouchStrip(device *streamdeck.Device, np *NowPlaying) {
	if !device.GetTouchStripSupported() {
		return
	}

	rect, err := device.GetTouchStripImageRectangle()
	if err != nil {
		return
	}

	img := image.NewRGBA(rect)

	// Background
	bgColor := color.RGBA{30, 30, 30, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// Progress
	progress := 0.0
	if np.Duration > 0 {
		progress = np.ElapsedTime / np.Duration
	}

	progressW := int(float64(rect.Dx()) * progress)
	progressColor := colornames.Limegreen
	if !np.Playing {
		progressColor = colornames.Orange
	}
	progressRect := image.Rect(0, 0, progressW, rect.Dy())
	draw.Draw(img, progressRect, &image.Uniform{progressColor}, image.Point{}, draw.Src)

	device.SetTouchStripImage(img)
}

func drawControlIcons(device *streamdeck.Device) {
	keyRect, _ := device.GetKeyImageRectangle()
	size := keyRect.Dx()

	// Previous track icon (⏮)
	device.SetKeyImage(keyPrev, drawPrevIcon(size))

	// Play/Pause icon (starts as play)
	drawPlayPauseIcon(device, false)

	// Next track icon (⏭)
	device.SetKeyImage(keyNext, drawNextIcon(size))

	// Info icon (ℹ)
	device.SetKeyImage(keyInfo, drawInfoIcon(size))
}

func drawPlayPauseIcon(device *streamdeck.Device, playing bool) {
	keyRect, _ := device.GetKeyImageRectangle()
	size := keyRect.Dx()

	if playing {
		device.SetKeyImage(keyPlay, drawPauseIcon(size))
	} else {
		device.SetKeyImage(keyPlay, drawPlayIcon(size))
	}
}

func drawPrevIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(img, color.RGBA{40, 40, 40, 255})

	// Draw ⏮ (two triangles pointing left + bar)
	iconColor := colornames.White
	center := size / 2
	iconSize := size / 3

	// Left bar
	barW := iconSize / 4
	fillRectArea(img, iconColor, center-iconSize, center-iconSize/2, barW, iconSize)

	// First triangle
	drawTriangleLeft(img, iconColor, center-iconSize/2, center, iconSize/2)

	// Second triangle
	drawTriangleLeft(img, iconColor, center+iconSize/4, center, iconSize/2)

	return img
}

func drawNextIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(img, color.RGBA{40, 40, 40, 255})

	iconColor := colornames.White
	center := size / 2
	iconSize := size / 3

	// First triangle
	drawTriangleRight(img, iconColor, center-iconSize/2, center, iconSize/2)

	// Second triangle
	drawTriangleRight(img, iconColor, center+iconSize/4, center, iconSize/2)

	// Right bar
	barW := iconSize / 4
	fillRectArea(img, iconColor, center+iconSize-barW, center-iconSize/2, barW, iconSize)

	return img
}

func drawPlayIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(img, color.RGBA{40, 40, 40, 255})

	// Draw triangle pointing right
	iconColor := colornames.Limegreen
	center := size / 2
	iconSize := size / 3

	drawTriangleRight(img, iconColor, center-iconSize/3, center, iconSize)

	return img
}

func drawPauseIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(img, color.RGBA{40, 40, 40, 255})

	// Draw two vertical bars
	iconColor := colornames.Orange
	center := size / 2
	barW := size / 8
	barH := size / 3
	gap := size / 8

	fillRectArea(img, iconColor, center-gap-barW, center-barH/2, barW, barH)
	fillRectArea(img, iconColor, center+gap, center-barH/2, barW, barH)

	return img
}

func drawInfoIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fillRect(img, color.RGBA{40, 40, 40, 255})

	// Draw "i" info symbol
	iconColor := colornames.Deepskyblue
	center := size / 2

	// Dot
	dotR := size / 12
	fillCircle(img, iconColor, center, center-size/5, dotR)

	// Stem
	stemW := size / 8
	stemH := size / 4
	fillRectArea(img, iconColor, center-stemW/2, center-size/12, stemW, stemH)

	return img
}

// Drawing helpers

func fillRect(img *image.RGBA, c color.Color) {
	draw.Draw(img, img.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
}

func fillRectArea(img *image.RGBA, c color.Color, x, y, w, h int) {
	rect := image.Rect(x, y, x+w, y+h)
	draw.Draw(img, rect, &image.Uniform{c}, image.Point{}, draw.Src)
}

func fillCircle(img *image.RGBA, c color.Color, cx, cy, r int) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx := x - cx
			dy := y - cy
			if dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

func drawTriangleRight(img *image.RGBA, c color.Color, x, cy, size int) {
	// Triangle pointing right with left edge at x, centered at cy
	for i := 0; i < size; i++ {
		// Width at this row
		halfH := (size - i) * size / (2 * size)
		for dy := -halfH; dy <= halfH; dy++ {
			img.Set(x+i, cy+dy, c)
		}
	}
}

func drawTriangleLeft(img *image.RGBA, c color.Color, x, cy, size int) {
	// Triangle pointing left with right edge at x, centered at cy
	for i := 0; i < size; i++ {
		halfH := (size - i) * size / (2 * size)
		for dy := -halfH; dy <= halfH; dy++ {
			img.Set(x-i, cy+dy, c)
		}
	}
}

func setupKeyControls(device *streamdeck.Device) {
	// Previous track
	device.AddKeyHandler(keyPrev, func(d *streamdeck.Device, k *streamdeck.Key) error {
		log.Println("Key: Previous track")
		exec.Command("media-control", "previous-track").Run()
		k.WaitForRelease()
		return nil
	})

	// Play/Pause
	device.AddKeyHandler(keyPlay, func(d *streamdeck.Device, k *streamdeck.Key) error {
		log.Println("Key: Toggle play/pause")
		exec.Command("media-control", "toggle-play-pause").Run()
		k.WaitForRelease()
		return nil
	})

	// Next track
	device.AddKeyHandler(keyNext, func(d *streamdeck.Device, k *streamdeck.Key) error {
		log.Println("Key: Next track")
		exec.Command("media-control", "next-track").Run()
		k.WaitForRelease()
		return nil
	})

	// Info - could show track details or toggle something
	device.AddKeyHandler(keyInfo, func(d *streamdeck.Device, k *streamdeck.Key) error {
		np, _ := getNowPlaying()
		if np != nil {
			log.Printf("Info: %s - %s (%s)", np.Artist, np.Title, np.Album)
		}
		k.WaitForRelease()
		return nil
	})

	log.Println("Key controls configured")
}

func setupDialControls(device *streamdeck.Device) {
	if device.GetDialCount() == 0 {
		return
	}

	// Dial 1: Seek
	device.AddDialRotateHandler(streamdeck.DIAL_1, func(d *streamdeck.Device, di *streamdeck.Dial, delta int8) error {
		seekAmount := int(delta) * 5
		log.Printf("Dial: Seeking %+d seconds", seekAmount)

		np, err := getNowPlaying()
		if err != nil {
			return nil
		}

		newPos := np.ElapsedTime + float64(seekAmount)
		if newPos < 0 {
			newPos = 0
		}
		if newPos > np.Duration {
			newPos = np.Duration
		}

		cmd := exec.Command("media-control", "seek", fmt.Sprintf("%.0f", newPos*1000000))
		cmd.Run()
		return nil
	})

	// Dial 1 press: Play/Pause
	device.AddDialSwitchHandler(streamdeck.DIAL_1, func(d *streamdeck.Device, di *streamdeck.Dial) error {
		log.Println("Dial: Toggle play/pause")
		exec.Command("media-control", "toggle-play-pause").Run()
		di.WaitForRelease()
		return nil
	})

	// Dial 2: Previous/Next
	device.AddDialRotateHandler(streamdeck.DIAL_2, func(d *streamdeck.Device, di *streamdeck.Dial, delta int8) error {
		if delta < 0 {
			log.Println("Dial: Previous track")
			exec.Command("media-control", "previous-track").Run()
		} else {
			log.Println("Dial: Next track")
			exec.Command("media-control", "next-track").Run()
		}
		return nil
	})

	log.Println("Dial controls: D1=seek/play-pause, D2=prev/next")
}
