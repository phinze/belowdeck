// Package emulator provides a GUI-based Stream Deck Plus emulator.
package emulator

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/phinze/belowdeck/internal/device"
)

// Layout constants for Stream Deck Plus
const (
	keySize        = 72  // Key image size (72x72) - native resolution
	keyDisplaySize = 144 // Key display size - clean 2x scale for crisp rendering
	keysPerRow     = 4   // Keys per row
	keyRows        = 2   // Number of key rows
	keyCount       = 8   // Total keys
	dialCount      = 4   // Total dials
	dialSize       = 120 // Visual dial size - similar to key size
	marginX        = 20 // Left/right margin
	marginY        = 20 // Top margin
	headerHeight   = 30 // Title bar height
	stripMarginY   = 72 // Space between keys and strip (~half key height)
	dialMarginY    = 50 // Space between strip and dials
	bottomMarginY  = 50 // Space below dials

	// Strip dimensions (native resolution)
	stripWidth  = 800
	stripHeight = 100
)

// Calculate layout - strip-native width, keys 2x scaled with remaining space as padding
const (
	keyAreaWidth  = keysPerRow * keyDisplaySize                                   // 4*144 = 576
	keySpacing    = (stripWidth - keyAreaWidth) / (keysPerRow + 1)                // Distribute remaining 224px as spacing = 44px each
	keyAreaHeight = keyRows*keyDisplaySize + (keyRows-1)*keySpacing               // 2*144 + 44 = 332
	dialSpacing   = (stripWidth - dialCount*dialSize) / (dialCount + 1)           // Even spacing for dials
	windowWidth   = 2*marginX + stripWidth
	windowHeight  = headerHeight + marginY + keyAreaHeight + stripMarginY + stripHeight + dialMarginY + dialSize + bottomMarginY
)

// Emulator implements the device.Device interface using Ebitengine for GUI rendering.
type Emulator struct {
	mu sync.RWMutex

	// State
	open       bool
	brightness byte
	keyImages  [keyCount]*image.RGBA
	stripImage *image.RGBA

	// Handlers
	keyHandlers         [keyCount][]device.KeyHandler
	dialRotateHandlers  [dialCount][]device.DialRotateHandler
	dialSwitchHandlers  [dialCount][]device.DialSwitchHandler
	stripTouchHandlers  []device.TouchStripTouchHandler
	stripSwipeHandlers  []device.TouchStripSwipeHandler

	// Ebitengine state
	game       *emulatorGame
	stopCh     chan struct{}
	errorCh    chan error
	listenDone chan struct{}

	// Input state (managed by game loop)
	prevMousePressed bool
	dragStart        image.Point
	dragStartTime    time.Time
	dragging         bool
}

// New creates a new emulator instance.
func New() *Emulator {
	e := &Emulator{
		brightness: 80,
		stopCh:     make(chan struct{}),
	}

	// Initialize key images to black
	for i := 0; i < keyCount; i++ {
		e.keyImages[i] = image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	}

	// Initialize strip image
	e.stripImage = image.NewRGBA(image.Rect(0, 0, stripWidth, stripHeight))

	return e
}

// Open initializes the emulator.
func (e *Emulator) Open() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.open {
		return fmt.Errorf("emulator: device is already open")
	}

	e.open = true
	e.stopCh = make(chan struct{})
	return nil
}

// Close shuts down the emulator.
func (e *Emulator) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.open {
		return fmt.Errorf("emulator: device is not open")
	}

	e.open = false

	// Signal the game loop to stop
	close(e.stopCh)

	return nil
}

// IsOpen returns whether the emulator is open.
func (e *Emulator) IsOpen() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.open
}

// GetModelName returns the emulated model name.
func (e *Emulator) GetModelName() string {
	return "Stream Deck Plus (Emulator)"
}

// GetKeyCount returns the number of keys.
func (e *Emulator) GetKeyCount() byte {
	return keyCount
}

// GetDialCount returns the number of dials.
func (e *Emulator) GetDialCount() byte {
	return dialCount
}

// GetTouchStripSupported returns true as the emulated device supports touch strip.
func (e *Emulator) GetTouchStripSupported() bool {
	return true
}

// GetKeyImageRectangle returns the key image dimensions.
func (e *Emulator) GetKeyImageRectangle() (image.Rectangle, error) {
	return image.Rect(0, 0, keySize, keySize), nil
}

// GetTouchStripImageRectangle returns the touch strip dimensions.
func (e *Emulator) GetTouchStripImageRectangle() (image.Rectangle, error) {
	return image.Rect(0, 0, stripWidth, stripHeight), nil
}

// SetBrightness sets the display brightness.
func (e *Emulator) SetBrightness(perc byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.brightness = perc
	return nil
}

// SetKeyImage sets the image for a key.
func (e *Emulator) SetKeyImage(key device.KeyID, img image.Image) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := int(key) - 1
	if idx < 0 || idx >= keyCount {
		return fmt.Errorf("emulator: invalid key ID: %d", key)
	}

	// Create new RGBA image and draw the provided image onto it
	rgba := image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	e.keyImages[idx] = rgba

	return nil
}

// SetTouchStripImage sets the touch strip image.
func (e *Emulator) SetTouchStripImage(img image.Image) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Create new RGBA image and draw the provided image onto it
	rgba := image.NewRGBA(image.Rect(0, 0, stripWidth, stripHeight))
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	e.stripImage = rgba

	return nil
}

// ClearKey clears a key's image to black.
func (e *Emulator) ClearKey(key device.KeyID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := int(key) - 1
	if idx < 0 || idx >= keyCount {
		return fmt.Errorf("emulator: invalid key ID: %d", key)
	}

	e.keyImages[idx] = image.NewRGBA(image.Rect(0, 0, keySize, keySize))
	return nil
}

// ForEachKey calls the callback for each key.
func (e *Emulator) ForEachKey(cb func(device.KeyID) error) error {
	for i := device.KEY_1; i <= device.KEY_8; i++ {
		if err := cb(i); err != nil {
			return err
		}
	}
	return nil
}

// ForEachDial calls the callback for each dial.
func (e *Emulator) ForEachDial(cb func(device.DialID) error) error {
	for i := device.DIAL_1; i <= device.DIAL_4; i++ {
		if err := cb(i); err != nil {
			return err
		}
	}
	return nil
}

// AddKeyHandler registers a key press handler.
func (e *Emulator) AddKeyHandler(key device.KeyID, fn device.KeyHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := int(key) - 1
	if idx < 0 || idx >= keyCount {
		return fmt.Errorf("emulator: invalid key ID: %d", key)
	}

	e.keyHandlers[idx] = append(e.keyHandlers[idx], fn)
	return nil
}

// AddDialRotateHandler registers a dial rotation handler.
func (e *Emulator) AddDialRotateHandler(dial device.DialID, fn device.DialRotateHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := int(dial) - 1
	if idx < 0 || idx >= dialCount {
		return fmt.Errorf("emulator: invalid dial ID: %d", dial)
	}

	e.dialRotateHandlers[idx] = append(e.dialRotateHandlers[idx], fn)
	return nil
}

// AddDialSwitchHandler registers a dial press handler.
func (e *Emulator) AddDialSwitchHandler(dial device.DialID, fn device.DialSwitchHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx := int(dial) - 1
	if idx < 0 || idx >= dialCount {
		return fmt.Errorf("emulator: invalid dial ID: %d", dial)
	}

	e.dialSwitchHandlers[idx] = append(e.dialSwitchHandlers[idx], fn)
	return nil
}

// AddTouchStripTouchHandler registers a touch strip touch handler.
func (e *Emulator) AddTouchStripTouchHandler(fn device.TouchStripTouchHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stripTouchHandlers = append(e.stripTouchHandlers, fn)
	return nil
}

// AddTouchStripSwipeHandler registers a touch strip swipe handler.
func (e *Emulator) AddTouchStripSwipeHandler(fn device.TouchStripSwipeHandler) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stripSwipeHandlers = append(e.stripSwipeHandlers, fn)
	return nil
}

// Listen blocks until the emulator is closed.
// For the emulator, the actual event loop runs via RunGUI() which must be called from main.
func (e *Emulator) Listen(errCh chan error) error {
	e.mu.Lock()
	if !e.open {
		e.mu.Unlock()
		return fmt.Errorf("emulator: device is not open")
	}
	e.errorCh = errCh
	if e.listenDone == nil {
		e.listenDone = make(chan struct{})
	}
	e.mu.Unlock()

	// Block until GUI is closed
	<-e.listenDone
	return nil
}

// RunGUI starts the Ebitengine GUI loop. This MUST be called from the main goroutine
// on macOS due to Cocoa threading requirements. This method blocks until the window is closed.
func (e *Emulator) RunGUI() error {
	e.mu.Lock()
	if !e.open {
		e.mu.Unlock()
		return fmt.Errorf("emulator: device is not open")
	}
	if e.listenDone == nil {
		e.listenDone = make(chan struct{})
	}
	e.game = &emulatorGame{emu: e}
	e.mu.Unlock()

	ebiten.SetWindowSize(windowWidth, windowHeight)
	ebiten.SetWindowTitle("Stream Deck Plus Emulator")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)

	// Run the game loop (this blocks until the window is closed)
	err := ebiten.RunGame(e.game)

	// Signal Listen() to unblock
	close(e.listenDone)
	return err
}

// emulatorGame implements ebiten.Game for the emulator.
type emulatorGame struct {
	emu *emulatorGame_emu
}

// We need a separate reference to avoid import cycle
type emulatorGame_emu = Emulator

func (g *emulatorGame) Update() error {
	// Check for stop signal
	select {
	case <-g.emu.stopCh:
		return ebiten.Termination
	default:
	}

	g.handleInput()
	return nil
}

func (g *emulatorGame) Draw(screen *ebiten.Image) {
	// Background
	screen.Fill(color.RGBA{30, 30, 30, 255})

	g.emu.mu.RLock()
	defer g.emu.mu.RUnlock()

	// Layout: keys centered with even spacing, strip at native width below
	keysStartX := marginX + keySpacing // First key offset by spacing
	keysStartY := headerHeight + marginY
	stripStartX := marginX
	stripStartY := keysStartY + keyAreaHeight + stripMarginY
	dialStartY := stripStartY + stripHeight + dialMarginY

	// Draw title
	ebitenutil.DebugPrintAt(screen, "Stream Deck Plus Emulator", windowWidth/2-100, 8)

	// Draw keys - clean 2x scale (72 -> 144) using nearest-neighbor
	for i := 0; i < keyCount; i++ {
		row := i / keysPerRow
		col := i % keysPerRow

		x := keysStartX + col*(keyDisplaySize+keySpacing)
		y := keysStartY + row*(keyDisplaySize+keySpacing)

		// Draw key background (border)
		drawRect(screen, x-2, y-2, keyDisplaySize+4, keyDisplaySize+4, color.RGBA{60, 60, 60, 255})

		// Draw key image scaled up with nearest-neighbor filtering
		if g.emu.keyImages[i] != nil {
			// Scale up using nearest-neighbor for crisp 2x scaling
			scaledImg := scaleImageNearest(g.emu.keyImages[i], keyDisplaySize, keyDisplaySize)
			keyImg := ebiten.NewImageFromImage(scaledImg)
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(x), float64(y))
			// Apply brightness
			brightness := float64(g.emu.brightness) / 100.0
			op.ColorScale.Scale(float32(brightness), float32(brightness), float32(brightness), 1)
			screen.DrawImage(keyImg, op)
		}
	}

	// Draw touch strip background
	drawRect(screen, stripStartX-2, stripStartY-2, stripWidth+4, stripHeight+4, color.RGBA{60, 60, 60, 255})

	// Draw touch strip image at native resolution
	if g.emu.stripImage != nil {
		stripImg := ebiten.NewImageFromImage(g.emu.stripImage)
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Translate(float64(stripStartX), float64(stripStartY))
		brightness := float64(g.emu.brightness) / 100.0
		op.ColorScale.Scale(float32(brightness), float32(brightness), float32(brightness), 1)
		screen.DrawImage(stripImg, op)
	}

	// Draw dials - evenly spaced across strip width
	for i := 0; i < dialCount; i++ {
		x := stripStartX + dialSpacing + i*(dialSize+dialSpacing)
		y := dialStartY

		// Calculate dial center
		cx := x + dialSize/2
		cy := y + dialSize/2
		radius := dialSize / 2

		// Draw dial as concentric circles (outer ring, inner dial)
		drawCircle(screen, cx, cy, radius, color.RGBA{80, 80, 80, 255})
		drawCircle(screen, cx, cy, radius-8, color.RGBA{50, 50, 50, 255})
		drawCircle(screen, cx, cy, radius-12, color.RGBA{70, 70, 70, 255})

		// Draw dial label
		label := fmt.Sprintf("D%d", i+1)
		ebitenutil.DebugPrintAt(screen, label, cx-8, cy-4)
	}

	// Draw instructions
	instrY := windowHeight - 18
	ebitenutil.DebugPrintAt(screen, "Click keys | Scroll over dials | Click/drag touch strip", 10, instrY)
}

func (g *emulatorGame) Layout(outsideWidth, outsideHeight int) (int, int) {
	return windowWidth, windowHeight
}

func (g *emulatorGame) handleInput() {
	mx, my := ebiten.CursorPosition()
	mousePressed := ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	// Calculate positions (same layout as in Draw)
	keysStartX := marginX + keySpacing // First key offset by spacing
	keysStartY := headerHeight + marginY
	stripStartX := marginX
	stripStartY := keysStartY + keyAreaHeight + stripMarginY
	dialStartY := stripStartY + stripHeight + dialMarginY

	// Handle key clicks
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		// Check if click is on a key
		for i := 0; i < keyCount; i++ {
			row := i / keysPerRow
			col := i % keysPerRow

			kx := keysStartX + col*(keyDisplaySize+keySpacing)
			ky := keysStartY + row*(keyDisplaySize+keySpacing)

			if mx >= kx && mx < kx+keyDisplaySize && my >= ky && my < ky+keyDisplaySize {
				g.triggerKeyPress(device.KeyID(i + 1))
				return
			}
		}

		// Check if click is on a dial (circular hit detection)
		for i := 0; i < dialCount; i++ {
			dx := stripStartX + dialSpacing + i*(dialSize+dialSpacing)
			dy := dialStartY
			// Dial center
			cx := dx + dialSize/2
			cy := dy + dialSize/2
			radius := dialSize / 2

			// Check if click is within circle
			distX := mx - cx
			distY := my - cy
			if distX*distX+distY*distY <= radius*radius {
				g.triggerDialPress(device.DialID(i + 1))
				return
			}
		}

		// Check if click is on touch strip - strip is at native resolution
		if mx >= stripStartX && mx < stripStartX+stripWidth && my >= stripStartY && my < stripStartY+stripHeight {
			g.emu.dragging = true
			// Coordinates are already in strip space (native resolution)
			g.emu.dragStart = image.Point{X: mx - stripStartX, Y: my - stripStartY}
			g.emu.dragStartTime = time.Now()
		}
	}

	// Handle touch strip drag/release
	if g.emu.dragging && !mousePressed {
		// Get end point in strip coordinates
		endX := mx - stripStartX
		endY := my - stripStartY

		// Clamp to strip bounds
		if endX < 0 {
			endX = 0
		}
		if endX >= stripWidth {
			endX = stripWidth - 1
		}
		if endY < 0 {
			endY = 0
		}
		if endY >= stripHeight {
			endY = stripHeight - 1
		}

		endPoint := image.Point{X: endX, Y: endY}
		duration := time.Since(g.emu.dragStartTime)

		// Calculate distance
		dx := endPoint.X - g.emu.dragStart.X
		dy := endPoint.Y - g.emu.dragStart.Y
		distSq := dx*dx + dy*dy

		// If distance is small, it's a tap
		if distSq < 400 { // Less than 20 pixel movement
			touchType := device.TOUCH_STRIP_TOUCH_TYPE_SHORT
			if duration > 500*time.Millisecond {
				touchType = device.TOUCH_STRIP_TOUCH_TYPE_LONG
			}
			g.triggerStripTouch(touchType, g.emu.dragStart)
		} else {
			// It's a swipe
			g.triggerStripSwipe(g.emu.dragStart, endPoint)
		}

		g.emu.dragging = false
	}

	// Handle scroll wheel for dial rotation (circular hit detection)
	_, wheelY := ebiten.Wheel()
	if wheelY != 0 {
		// Check which dial (if any) the cursor is over
		for i := 0; i < dialCount; i++ {
			dx := stripStartX + dialSpacing + i*(dialSize+dialSpacing)
			dy := dialStartY
			// Dial center
			cx := dx + dialSize/2
			cy := dy + dialSize/2
			radius := dialSize / 2

			// Check if cursor is within circle
			distX := mx - cx
			distY := my - cy
			if distX*distX+distY*distY <= radius*radius {
				delta := int8(wheelY)
				if delta > 5 {
					delta = 5
				} else if delta < -5 {
					delta = -5
				}
				g.triggerDialRotate(device.DialID(i+1), delta)
				break
			}
		}
	}

	g.emu.prevMousePressed = mousePressed
}

func (g *emulatorGame) triggerKeyPress(keyID device.KeyID) {
	g.emu.mu.RLock()
	handlers := g.emu.keyHandlers[int(keyID)-1]
	g.emu.mu.RUnlock()

	for _, handler := range handlers {
		key := &emulatorKey{
			id:        keyID,
			releaseCh: make(chan struct{}),
		}

		// Fire handler in goroutine
		go func(h device.KeyHandler, k *emulatorKey) {
			if err := h(g.emu, k); err != nil {
				if g.emu.errorCh != nil {
					select {
					case g.emu.errorCh <- err:
					default:
					}
				}
			}
		}(handler, key)

		// Simulate immediate release for click (not hold)
		go func(k *emulatorKey) {
			time.Sleep(50 * time.Millisecond)
			k.release()
		}(key)
	}
}

func (g *emulatorGame) triggerDialPress(dialID device.DialID) {
	g.emu.mu.RLock()
	handlers := g.emu.dialSwitchHandlers[int(dialID)-1]
	g.emu.mu.RUnlock()

	for _, handler := range handlers {
		dial := &emulatorDial{
			id:        dialID,
			releaseCh: make(chan struct{}),
		}

		go func(h device.DialSwitchHandler, d *emulatorDial) {
			if err := h(g.emu, d); err != nil {
				if g.emu.errorCh != nil {
					select {
					case g.emu.errorCh <- err:
					default:
					}
				}
			}
		}(handler, dial)

		go func(d *emulatorDial) {
			time.Sleep(50 * time.Millisecond)
			d.release()
		}(dial)
	}
}

func (g *emulatorGame) triggerDialRotate(dialID device.DialID, delta int8) {
	g.emu.mu.RLock()
	handlers := g.emu.dialRotateHandlers[int(dialID)-1]
	g.emu.mu.RUnlock()

	for _, handler := range handlers {
		dial := &emulatorDial{
			id:        dialID,
			releaseCh: make(chan struct{}),
		}

		go func(h device.DialRotateHandler, d *emulatorDial, delta int8) {
			if err := h(g.emu, d, delta); err != nil {
				if g.emu.errorCh != nil {
					select {
					case g.emu.errorCh <- err:
					default:
					}
				}
			}
		}(handler, dial, delta)
	}
}

func (g *emulatorGame) triggerStripTouch(touchType device.TouchStripTouchType, point image.Point) {
	g.emu.mu.RLock()
	handlers := g.emu.stripTouchHandlers
	g.emu.mu.RUnlock()

	for _, handler := range handlers {
		go func(h device.TouchStripTouchHandler) {
			if err := h(g.emu, touchType, point); err != nil {
				if g.emu.errorCh != nil {
					select {
					case g.emu.errorCh <- err:
					default:
					}
				}
			}
		}(handler)
	}
}

func (g *emulatorGame) triggerStripSwipe(origin, destination image.Point) {
	g.emu.mu.RLock()
	handlers := g.emu.stripSwipeHandlers
	g.emu.mu.RUnlock()

	for _, handler := range handlers {
		go func(h device.TouchStripSwipeHandler) {
			if err := h(g.emu, origin, destination); err != nil {
				if g.emu.errorCh != nil {
					select {
					case g.emu.errorCh <- err:
					default:
					}
				}
			}
		}(handler)
	}
}

// emulatorKey implements device.Key for the emulator.
type emulatorKey struct {
	id          device.KeyID
	releaseCh   chan struct{}
	releaseOnce sync.Once
	pressTime   time.Time
}

func (k *emulatorKey) GetID() device.KeyID {
	return k.id
}

func (k *emulatorKey) WaitForRelease() time.Duration {
	k.pressTime = time.Now()
	<-k.releaseCh
	return time.Since(k.pressTime)
}

func (k *emulatorKey) release() {
	k.releaseOnce.Do(func() {
		close(k.releaseCh)
	})
}

// emulatorDial implements device.Dial for the emulator.
type emulatorDial struct {
	id          device.DialID
	releaseCh   chan struct{}
	releaseOnce sync.Once
	pressTime   time.Time
}

func (d *emulatorDial) GetID() device.DialID {
	return d.id
}

func (d *emulatorDial) WaitForRelease() time.Duration {
	d.pressTime = time.Now()
	<-d.releaseCh
	return time.Since(d.pressTime)
}

func (d *emulatorDial) release() {
	d.releaseOnce.Do(func() {
		close(d.releaseCh)
	})
}

// Helper function to draw a filled rectangle
func drawRect(screen *ebiten.Image, x, y, w, h int, c color.Color) {
	rect := ebiten.NewImage(w, h)
	rect.Fill(c)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	screen.DrawImage(rect, op)
}

// Helper function to draw a filled circle
func drawCircle(screen *ebiten.Image, cx, cy, radius int, c color.Color) {
	diameter := radius * 2
	circle := ebiten.NewImage(diameter, diameter)

	r, g, b, a := c.RGBA()
	col := color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}

	for y := 0; y < diameter; y++ {
		for x := 0; x < diameter; x++ {
			dx := x - radius
			dy := y - radius
			if dx*dx+dy*dy <= radius*radius {
				circle.Set(x, y, col)
			}
		}
	}

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(cx-radius), float64(cy-radius))
	screen.DrawImage(circle, op)
}

// scaleImageNearest scales an image using nearest-neighbor interpolation for crisp pixel scaling.
func scaleImageNearest(src *image.RGBA, newWidth, newHeight int) *image.RGBA {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			// Map destination pixel to source pixel (nearest neighbor)
			srcX := x * srcW / newWidth
			srcY := y * srcH / newHeight
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	return dst
}
