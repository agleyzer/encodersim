// Package playlist implements live HLS playlist generation with sliding window support.
package playlist

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agleyzer/encodersim/internal/segment"
	"github.com/agleyzer/encodersim/internal/variant"
)

// LivePlaylist manages a sliding window over a list of segments
// and generates live HLS playlists.
// Supports both single media playlist mode and multi-variant master playlist mode.
type LivePlaylist struct {
	mu              sync.RWMutex
	isMaster        bool
	variants        []variant.Variant
	variantPos      []int             // Per-variant current positions (for master mode)
	segments        []segment.Segment // For media playlist mode (backward compatibility)
	windowSize      int
	currentPosition int    // For media playlist mode
	sequenceNumber  uint64 // Global sequence number
	targetDuration  int
	logger          *slog.Logger
}

// New creates a new LivePlaylist.
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
		isMaster:        false,
		segments:        segments,
		windowSize:      windowSize,
		currentPosition: 0,
		sequenceNumber:  0,
		targetDuration:  targetDuration,
		logger:          logger,
	}, nil
}

// NewMaster creates a new LivePlaylist for master playlists with multiple variants.
func NewMaster(variants []variant.Variant, windowSize int, logger *slog.Logger) (*LivePlaylist, error) {
	if len(variants) == 0 {
		return nil, fmt.Errorf("cannot create master playlist with zero variants")
	}

	if windowSize <= 0 {
		return nil, fmt.Errorf("window size must be positive")
	}

	// Calculate max target duration across all variants
	maxTargetDuration := 0
	for _, v := range variants {
		if v.TargetDuration > maxTargetDuration {
			maxTargetDuration = v.TargetDuration
		}
	}

	// Validate that all variants have segments
	for i, v := range variants {
		if len(v.Segments) == 0 {
			return nil, fmt.Errorf("variant %d has zero segments", i)
		}

		// Adjust window size if larger than variant segment count
		if windowSize > len(v.Segments) {
			logger.Warn("window size larger than variant segment count",
				"variant", i,
				"windowSize", windowSize,
				"segmentCount", len(v.Segments),
			)
		}
	}

	// Initialize per-variant positions to 0
	variantPos := make([]int, len(variants))

	return &LivePlaylist{
		isMaster:       true,
		variants:       variants,
		variantPos:     variantPos,
		windowSize:     windowSize,
		sequenceNumber: 0,
		targetDuration: maxTargetDuration,
		logger:         logger,
	}, nil
}

// GenerateMaster creates an HLS master playlist with variant streams.
// This should only be called when isMaster is true.
func (lp *LivePlaylist) GenerateMaster() string {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	var b strings.Builder

	// HLS master playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")

	// Write variant streams
	for i, v := range lp.variants {
		// Build #EXT-X-STREAM-INF attributes
		b.WriteString("#EXT-X-STREAM-INF:")
		b.WriteString(fmt.Sprintf("BANDWIDTH=%d", v.Bandwidth))

		if v.Resolution != "" {
			b.WriteString(fmt.Sprintf(",RESOLUTION=%s", v.Resolution))
		}

		if v.Codecs != "" {
			b.WriteString(fmt.Sprintf(",CODECS=\"%s\"", v.Codecs))
		}

		b.WriteString("\n")

		// Write variant playlist URL
		b.WriteString(fmt.Sprintf("/variant%d/playlist.m3u8\n", i))
	}

	return b.String()
}

// Generate creates an HLS playlist for the current window.
// For media playlists (isMaster=false), this generates the media playlist.
// For master playlists, use GenerateMaster() instead.
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

// GenerateVariant creates an HLS media playlist for a specific variant.
// This should only be called when isMaster is true.
func (lp *LivePlaylist) GenerateVariant(variantIndex int) (string, error) {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	if !lp.isMaster {
		return "", fmt.Errorf("GenerateVariant called on non-master playlist")
	}

	if variantIndex < 0 || variantIndex >= len(lp.variants) {
		return "", fmt.Errorf("variant index %d out of range (0-%d)", variantIndex, len(lp.variants)-1)
	}

	variant := lp.variants[variantIndex]
	variantPos := lp.variantPos[variantIndex]

	var b strings.Builder

	// HLS playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", variant.TargetDuration))
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", lp.sequenceNumber))

	// Get current window of segments for this variant
	windowSegments := lp.getCurrentWindowForVariant(variantIndex, variantPos, variant.Segments)

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

	return b.String(), nil
}

// getCurrentWindow returns the current window of segments.
// Caller must hold at least a read lock.
func (lp *LivePlaylist) getCurrentWindow() []segment.Segment {
	totalSegments := len(lp.segments)
	window := make([]segment.Segment, 0, lp.windowSize)

	for i := 0; i < lp.windowSize; i++ {
		idx := (lp.currentPosition + i) % totalSegments
		window = append(window, lp.segments[idx])
	}

	return window
}

// getCurrentWindowForVariant returns the current window of segments for a specific variant.
// Caller must hold at least a read lock.
func (lp *LivePlaylist) getCurrentWindowForVariant(variantIndex, position int, segments []segment.Segment) []segment.Segment {
	totalSegments := len(segments)
	effectiveWindowSize := lp.windowSize
	if effectiveWindowSize > totalSegments {
		effectiveWindowSize = totalSegments
	}

	window := make([]segment.Segment, 0, effectiveWindowSize)

	for i := 0; i < effectiveWindowSize; i++ {
		idx := (position + i) % totalSegments
		window = append(window, segments[idx])
	}

	return window
}

// Advance moves the sliding window forward by one segment.
// For master playlists, advances all variants synchronously.
func (lp *LivePlaylist) Advance() {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	if lp.isMaster {
		// Advance all variants
		for i := range lp.variants {
			totalSegments := len(lp.variants[i].Segments)
			lp.variantPos[i] = (lp.variantPos[i] + 1) % totalSegments
		}

		lp.sequenceNumber++

		lp.logger.Debug("advanced all variant windows",
			"variants", len(lp.variants),
			"sequence", lp.sequenceNumber,
		)
	} else {
		// Media playlist mode
		totalSegments := len(lp.segments)
		lp.currentPosition = (lp.currentPosition + 1) % totalSegments
		lp.sequenceNumber++

		lp.logger.Debug("advanced window",
			"position", lp.currentPosition,
			"sequence", lp.sequenceNumber,
		)
	}
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration.
// For master playlists, uses the maximum target duration across all variants.
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

// GetStats returns current statistics about the playlist.
// For master playlists, includes per-variant statistics.
func (lp *LivePlaylist) GetStats() map[string]interface{} {
	lp.mu.RLock()
	defer lp.mu.RUnlock()

	stats := map[string]interface{}{
		"is_master":       lp.isMaster,
		"window_size":     lp.windowSize,
		"sequence_number": lp.sequenceNumber,
		"target_duration": lp.targetDuration,
	}

	if lp.isMaster {
		// Master playlist stats
		variantStats := make([]map[string]interface{}, len(lp.variants))
		for i, v := range lp.variants {
			variantStats[i] = map[string]interface{}{
				"index":          i,
				"bandwidth":      v.Bandwidth,
				"resolution":     v.Resolution,
				"total_segments": len(v.Segments),
				"position":       lp.variantPos[i],
			}
		}
		stats["variants"] = variantStats
		stats["variant_count"] = len(lp.variants)
	} else {
		// Media playlist stats
		stats["total_segments"] = len(lp.segments)
		stats["current_position"] = lp.currentPosition
	}

	return stats
}
