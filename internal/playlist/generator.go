package playlist

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agleyzer/encodersim/pkg/segment"
)

// LivePlaylist manages a sliding window over a list of segments
// and generates live HLS playlists
type LivePlaylist struct {
	mu              sync.RWMutex
	segments        []segment.Segment
	windowSize      int
	currentPosition int
	sequenceNumber  uint64
	targetDuration  int
	logger          *slog.Logger
}

// New creates a new LivePlaylist
func New(segments []segment.Segment, windowSize, targetDuration int, logger *slog.Logger) (*LivePlaylist, error) {
	if len(segments) == 0 {
		return nil, fmt.Errorf("cannot create playlist with zero segments")
	}

	if windowSize <= 0 {
		return nil, fmt.Errorf("window size must be positive")
	}

	if windowSize > len(segments) {
		windowSize = len(segments)
		logger.Warn("window size larger than segment count, using all segments", "windowSize", windowSize)
	}

	return &LivePlaylist{
		segments:       segments,
		windowSize:     windowSize,
		currentPosition: 0,
		sequenceNumber: 0,
		targetDuration: targetDuration,
		logger:         logger,
	}, nil
}

// Generate creates an HLS playlist for the current window
func (lp *LivePlaylist) Generate() string {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	var b strings.Builder

	// HLS playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", lp.targetDuration))
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", lp.sequenceNumber))

	// Get current window of segments
	windowSegments := lp.getCurrentWindow()

	// Write segments with discontinuity detection
	for i, seg := range windowSegments {
		// Check for discontinuity (loop point)
		// If this segment's sequence is less than the previous segment's,
		// we've wrapped around to the beginning
		if i > 0 && seg.Sequence < windowSegments[i-1].Sequence {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}

		b.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", seg.Duration))
		b.WriteString(seg.URL)
		b.WriteString("\n")
	}

	// NOTE: We do NOT include #EXT-X-ENDLIST because this is a live stream

	return b.String()
}

// getCurrentWindow returns the current window of segments
// Caller must hold at least a read lock
func (lp *LivePlaylist) getCurrentWindow() []segment.Segment {
	totalSegments := len(lp.segments)
	window := make([]segment.Segment, 0, lp.windowSize)

	for i := 0; i < lp.windowSize; i++ {
		idx := (lp.currentPosition + i) % totalSegments
		window = append(window, lp.segments[idx])
	}

	return window
}

// Advance moves the sliding window forward by one segment
func (lp *LivePlaylist) Advance() {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	totalSegments := len(lp.segments)
	lp.currentPosition = (lp.currentPosition + 1) % totalSegments
	lp.sequenceNumber++

	lp.logger.Debug("advanced window",
		"position", lp.currentPosition,
		"sequence", lp.sequenceNumber,
	)
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration
func (lp *LivePlaylist) StartAutoAdvance(ctx context.Context) {
	// Use target duration as the advancement interval
	interval := time.Duration(lp.targetDuration) * time.Second

	lp.logger.Info("starting auto-advance",
		"interval", interval,
		"windowSize", lp.windowSize,
		"totalSegments", len(lp.segments),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lp.logger.Info("stopping auto-advance")
			return
		case <-ticker.C:
			lp.Advance()
		}
	}
}

// GetStats returns current statistics about the playlist
func (lp *LivePlaylist) GetStats() map[string]interface{} {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	return map[string]interface{}{
		"total_segments":   len(lp.segments),
		"window_size":      lp.windowSize,
		"current_position": lp.currentPosition,
		"sequence_number":  lp.sequenceNumber,
		"target_duration":  lp.targetDuration,
	}
}
