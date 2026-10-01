// Package github provides a Stream Deck module for GitHub PR stats.
package github

import (
	"context"
	"image"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/phinze/belowdeck/internal/device"
	"github.com/phinze/belowdeck/internal/module"
	"golang.org/x/image/font"
)

// OverlayType indicates which overlay is currently active.
type OverlayType int

const (
	OverlayNone OverlayType = iota
	OverlayMyPRs
	OverlayReviewRequested
)

// itemsPerPage is how many PRs fit on the overlay at once, one per key.
const itemsPerPage = 8

// Module implements the GitHub PR stats module.
type Module struct {
	module.BaseModule

	device  device.Device
	client  *Client
	enabled bool

	// PR lists for my PRs (Key3) and review-requested PRs (Key4). Key3's
	// counts are derived from prList on each fetch.
	mu           sync.RWMutex
	stats        PRStats
	prList       []PRInfo
	reviewPRList []PRInfo

	// Overlay state
	overlayType   OverlayType
	overlayExpiry time.Time
	currentPage   int // Current page in pagination (0-indexed)

	// Fonts
	labelFace      font.Face
	numberFace     font.Face
	overlayFace    font.Face
	stripTitleFace font.Face
	stripLabelFace font.Face

	// Resources
	resources module.Resources

	// Context for fetching
	ctx context.Context
}

// New creates a new GitHub module.
func New(dev device.Device) *Module {
	return &Module{
		BaseModule: module.NewBaseModule("github"),
		device:     dev,
	}
}

// ID returns the module identifier.
func (m *Module) ID() string {
	return "github"
}

// Init initializes the module.
func (m *Module) Init(ctx context.Context, res module.Resources) error {
	if err := m.BaseModule.Init(ctx, res); err != nil {
		return err
	}

	m.resources = res
	m.ctx = ctx

	// Create API client (uses gh CLI token)
	client, err := NewClient()
	if err != nil {
		log.Printf("GitHub module disabled: %v", err)
		m.enabled = false
		return nil
	}
	m.client = client
	m.enabled = true

	// Initialize fonts
	if err := m.initFonts(); err != nil {
		return err
	}

	// Start polling
	go m.pollStats(ctx)

	log.Println("GitHub module initialized")
	return nil
}

// Stop shuts down the module.
func (m *Module) Stop() error {
	return m.BaseModule.Stop()
}

// pollStats periodically fetches PR stats from GitHub.
func (m *Module) pollStats(ctx context.Context) {
	// Initial fetch
	m.fetchStats(ctx)

	// Poll every 2 minutes (to avoid rate limits)
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.fetchStats(ctx)
		}
	}
}

// fetchStats refreshes both PR lists. A failed fetch keeps the previous list
// (and its counts) rather than blanking the keys.
func (m *Module) fetchStats(ctx context.Context) {
	prList, err := m.client.GetMyPRList(ctx)
	if err != nil {
		log.Printf("Failed to fetch GitHub PR list: %v", err)
	} else {
		m.mu.Lock()
		m.prList = prList
		m.stats = summarize(prList)
		m.mu.Unlock()
	}

	reviewPRList, err := m.client.GetReviewRequestedPRList(ctx)
	if err != nil {
		log.Printf("Failed to fetch review-requested PR list: %v", err)
	} else {
		m.mu.Lock()
		m.reviewPRList = reviewPRList
		m.mu.Unlock()
	}
}

// getStats returns the current PR stats.
func (m *Module) getStats() PRStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
}

// getReviewCount returns how many PRs are awaiting my review.
func (m *Module) getReviewCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.reviewPRList)
}

// overlayState returns the PR list for the active overlay and the current page.
func (m *Module) overlayState() ([]PRInfo, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.overlayType == OverlayReviewRequested {
		return m.reviewPRList, m.currentPage
	}
	return m.prList, m.currentPage
}

// RenderKeys returns images for the module's keys.
func (m *Module) RenderKeys() map[module.KeyID]image.Image {
	if !m.enabled {
		return nil
	}

	keys := make(map[module.KeyID]image.Image)

	// Key 0 (Key3): My PR stats overview (outbox)
	if len(m.resources.Keys) > 0 {
		keys[m.resources.Keys[0]] = m.renderPRStatsButton()
	}

	// Key 1 (Key4): Review-requested PRs (inbox)
	if len(m.resources.Keys) > 1 {
		keys[m.resources.Keys[1]] = m.renderReviewRequestedButton()
	}

	return keys
}

// RenderStrip returns the touch strip image.
func (m *Module) RenderStrip() image.Image {
	return nil
}

// HandleKey processes key events.
func (m *Module) HandleKey(id module.KeyID, event module.KeyEvent) error {
	// Only trigger on press (not release)
	if !event.Pressed {
		return nil
	}

	// Determine which overlay to show based on which key was pressed
	m.mu.Lock()
	if len(m.resources.Keys) > 1 && id == m.resources.Keys[1] {
		// Key4 pressed - show review-requested overlay
		m.overlayType = OverlayReviewRequested
	} else {
		// Key3 pressed - show my PRs overlay
		m.overlayType = OverlayMyPRs
	}
	m.overlayExpiry = time.Now().Add(5 * time.Second)
	m.currentPage = 0 // Reset to first page
	m.mu.Unlock()

	return nil
}

// HandleDial processes dial events.
func (m *Module) HandleDial(id module.DialID, event module.DialEvent) error {
	return nil
}

// HandleOverlayDial processes dial events when the overlay is active.
// Dial4 (right knob) controls pagination: rotate to change page, click to dismiss overlay.
func (m *Module) HandleOverlayDial(id module.DialID, event module.DialEvent) error {
	// Only handle Dial4 (right knob)
	if id != module.Dial4 {
		return nil
	}

	prList, _ := m.overlayState()
	totalPages := pageCount(len(prList))

	switch event.Type {
	case module.DialRotate:
		m.mu.Lock()
		// Rotate right (positive delta) = next page, left = previous page
		if event.Delta > 0 {
			m.currentPage++
			if m.currentPage >= totalPages {
				m.currentPage = totalPages - 1
			}
		} else if event.Delta < 0 {
			m.currentPage--
			if m.currentPage < 0 {
				m.currentPage = 0
			}
		}
		// Reset the 5s timer on page change
		m.overlayExpiry = time.Now().Add(5 * time.Second)
		m.mu.Unlock()

	case module.DialRelease:
		// Click dismisses the overlay
		m.mu.Lock()
		m.overlayType = OverlayNone
		m.mu.Unlock()
	}

	return nil
}

// HandleStripTouch processes touch strip events.
func (m *Module) HandleStripTouch(event module.TouchStripEvent) error {
	return nil
}

// HandleOverlayKey processes key events when the overlay is active.
func (m *Module) HandleOverlayKey(id module.KeyID, event module.KeyEvent) error {
	// Only trigger on press (not release)
	if !event.Pressed {
		return nil
	}

	prList, currentPage := m.overlayState()

	// Map key to PR index (Key1-Key8 map to PRs on current page)
	keyIndex := int(id) - 1 // Key1=1, so subtract 1 for 0-indexed
	prIndex := currentPage*itemsPerPage + keyIndex
	if prIndex >= 0 && prIndex < len(prList) {
		pr := prList[prIndex]
		if pr.URL != "" {
			m.openURL(pr.URL)
		}
	}

	return nil
}

// HandleOverlayStripTouch processes touch strip events when the overlay is active.
func (m *Module) HandleOverlayStripTouch(event module.TouchStripEvent) error {
	// Strip now shows repo summary (left) and pagination affordance (right)
	// Tapping the right side (pagination area) does nothing special
	// Users interact with PRs via the keys, and pagination via the right dial
	return nil
}

// openURL opens a URL in the default browser.
func (m *Module) openURL(url string) {
	// Run, not Start: `open` exits as soon as it hands off to the browser, and
	// with no Wait it lingers as a zombie for the life of the daemon. Run in a
	// goroutine to keep the key handler non-blocking, matching how the other
	// modules shell out.
	go func() {
		if err := exec.Command("open", url).Run(); err != nil {
			log.Printf("Failed to open URL %s: %v", url, err)
		}
	}()
}

// IsOverlayActive returns true if the PR list overlay is visible.
func (m *Module) IsOverlayActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.overlayType != OverlayNone && time.Now().After(m.overlayExpiry) {
		m.overlayType = OverlayNone
	}
	return m.overlayType != OverlayNone
}

// RenderOverlayKeys returns images for all 8 keys showing PR list with pagination.
func (m *Module) RenderOverlayKeys() map[module.KeyID]image.Image {
	keys := make(map[module.KeyID]image.Image)

	prList, currentPage := m.overlayState()

	// All 8 keys show PRs (back is via dial click)
	prKeys := []module.KeyID{
		module.Key1, module.Key2, module.Key3, module.Key4,
		module.Key5, module.Key6, module.Key7, module.Key8,
	}

	startIndex := currentPage * itemsPerPage
	for i, keyID := range prKeys {
		prIndex := startIndex + i
		if prIndex < len(prList) {
			keys[keyID] = m.renderPRKey(prList[prIndex])
		} else {
			keys[keyID] = m.renderEmptyKey()
		}
	}

	return keys
}

// RenderOverlayStrip returns the touch strip image for the overlay.
func (m *Module) RenderOverlayStrip() image.Image {
	prList, currentPage := m.overlayState()

	return m.renderOverlayStripWithPRs(prList, currentPage)
}
