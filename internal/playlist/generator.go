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

// Playlist defines the interface for HLS playlist generation.
// Implementations include mediaPlaylist (single media playlist)
// and multiVariantPlaylist (multi-variant master playlist).
type Playlist interface {
	// Generate creates an HLS playlist.
	// For media playlists, returns the media playlist content.
	// For master playlists, returns the master playlist content (convenience method).
	Generate() (string, error)

	// GenerateMaster creates an HLS master playlist with variant streams.
	// Returns an error if called on a non-master playlist.
	GenerateMaster() (string, error)

	// GenerateVariant creates an HLS media playlist for a specific variant.
	// Returns an error if called on a non-master playlist or if variant index is invalid.
	GenerateVariant(variantIndex int) (string, error)

	// Advance moves the sliding window forward by one segment.
	Advance()

	// StartAutoAdvance starts a goroutine that automatically advances the window
	// based on the target duration.
	StartAutoAdvance(ctx context.Context)

	// GetStats returns current statistics about the playlist.
	GetStats() map[string]interface{}
}

// New creates a new media playlist.
func New(segments []segment.Segment, windowSize, targetDuration int, logger *slog.Logger) (Playlist, error) {
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

	return &mediaPlaylist{
		segments:        segments,
		windowSize:      windowSize,
		currentPosition: 0,
		sequenceNumber:  0,
		targetDuration:  targetDuration,
		logger:          logger,
	}, nil
}

// NewMaster creates a new master playlist with multiple variants.
func NewMaster(variants []variant.Variant, windowSize int, logger *slog.Logger) (Playlist, error) {
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

	return &multiVariantPlaylist{
		variants:       variants,
		variantPos:     variantPos,
		windowSize:     windowSize,
		sequenceNumber: 0,
		targetDuration: maxTargetDuration,
		logger:         logger,
	}, nil
}

// mediaPlaylist manages a sliding window for a single media playlist.
type mediaPlaylist struct {
	mu              sync.RWMutex
	segments        []segment.Segment
	windowSize      int
	currentPosition int
	sequenceNumber  uint64
	targetDuration  int
	logger          *slog.Logger
}

// Generate creates an HLS media playlist for the current window.
func (mp *mediaPlaylist) Generate() (string, error) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var b strings.Builder

	// HLS playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", mp.targetDuration))
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", mp.sequenceNumber))

	// Get current window of segments
	windowSegments := mp.getCurrentWindow()

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

// GenerateMaster returns an error because this is not a master playlist.
func (mp *mediaPlaylist) GenerateMaster() (string, error) {
	return "", fmt.Errorf("not a master playlist")
}

// GenerateVariant returns an error because this is not a master playlist.
func (mp *mediaPlaylist) GenerateVariant(variantIndex int) (string, error) {
	return "", fmt.Errorf("not a master playlist")
}

// Advance moves the sliding window forward by one segment.
func (mp *mediaPlaylist) Advance() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	totalSegments := len(mp.segments)
	mp.currentPosition = (mp.currentPosition + 1) % totalSegments
	mp.sequenceNumber++

	mp.logger.Debug("advanced window",
		"position", mp.currentPosition,
		"sequence", mp.sequenceNumber,
	)
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration.
func (mp *mediaPlaylist) StartAutoAdvance(ctx context.Context) {
	// Use target duration as the advancement interval
	interval := time.Duration(mp.targetDuration) * time.Second

	mp.logger.Info("starting auto-advance",
		"interval", interval,
		"windowSize", mp.windowSize,
		"totalSegments", len(mp.segments),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mp.logger.Info("stopping auto-advance")
			return
		case <-ticker.C:
			mp.Advance()
		}
	}
}

// GetStats returns current statistics about the playlist.
func (mp *mediaPlaylist) GetStats() map[string]interface{} {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return map[string]interface{}{
		"is_master":        false,
		"window_size":      mp.windowSize,
		"sequence_number":  mp.sequenceNumber,
		"target_duration":  mp.targetDuration,
		"total_segments":   len(mp.segments),
		"current_position": mp.currentPosition,
	}
}

// getCurrentWindow returns the current window of segments.
// Caller must hold at least a read lock.
func (mp *mediaPlaylist) getCurrentWindow() []segment.Segment {
	totalSegments := len(mp.segments)
	window := make([]segment.Segment, 0, mp.windowSize)

	for i := 0; i < mp.windowSize; i++ {
		idx := (mp.currentPosition + i) % totalSegments
		window = append(window, mp.segments[idx])
	}

	return window
}

// multiVariantPlaylist manages a sliding window for a multi-variant master playlist.
// It generates both the master playlist (with variant links) and individual variant
// media playlists.
type multiVariantPlaylist struct {
	mu             sync.RWMutex
	variants       []variant.Variant
	variantPos     []int // Per-variant current positions
	windowSize     int
	sequenceNumber uint64
	targetDuration int
	logger         *slog.Logger
}

// Generate creates an HLS master playlist (convenience method).
// Delegates to GenerateMaster() for consistency.
func (mvp *multiVariantPlaylist) Generate() (string, error) {
	return mvp.GenerateMaster()
}

// GenerateMaster creates an HLS master playlist with variant streams.
func (mvp *multiVariantPlaylist) GenerateMaster() (string, error) {
	mvp.mu.RLock()
	defer mvp.mu.RUnlock()

	var b strings.Builder

	// HLS master playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")

	// Write variant streams
	for i, v := range mvp.variants {
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

	return b.String(), nil
}

// GenerateVariant creates an HLS media playlist for a specific variant.
func (mvp *multiVariantPlaylist) GenerateVariant(variantIndex int) (string, error) {
	mvp.mu.RLock()
	defer mvp.mu.RUnlock()

	if variantIndex < 0 || variantIndex >= len(mvp.variants) {
		return "", fmt.Errorf("variant index %d out of range (0-%d)", variantIndex, len(mvp.variants)-1)
	}

	variant := mvp.variants[variantIndex]
	variantPos := mvp.variantPos[variantIndex]

	var b strings.Builder

	// HLS playlist header
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", variant.TargetDuration))
	b.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", mvp.sequenceNumber))

	// Get current window of segments for this variant
	windowSegments := mvp.getCurrentWindowForVariant(variantPos, variant.Segments)

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

// Advance moves the sliding window forward by one segment for all variants.
func (mvp *multiVariantPlaylist) Advance() {
	mvp.mu.Lock()
	defer mvp.mu.Unlock()

	// Advance all variants synchronously
	for i := range mvp.variants {
		totalSegments := len(mvp.variants[i].Segments)
		mvp.variantPos[i] = (mvp.variantPos[i] + 1) % totalSegments
	}

	mvp.sequenceNumber++

	mvp.logger.Debug("advanced all variant windows",
		"variants", len(mvp.variants),
		"sequence", mvp.sequenceNumber,
	)
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration.
// Uses the maximum target duration across all variants.
func (mvp *multiVariantPlaylist) StartAutoAdvance(ctx context.Context) {
	// Use target duration as the advancement interval
	interval := time.Duration(mvp.targetDuration) * time.Second

	mvp.logger.Info("starting auto-advance",
		"interval", interval,
		"windowSize", mvp.windowSize,
		"variantCount", len(mvp.variants),
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			mvp.logger.Info("stopping auto-advance")
			return
		case <-ticker.C:
			mvp.Advance()
		}
	}
}

// GetStats returns current statistics about the playlist.
// Includes per-variant statistics.
func (mvp *multiVariantPlaylist) GetStats() map[string]interface{} {
	mvp.mu.RLock()
	defer mvp.mu.RUnlock()

	// Build per-variant stats
	variantStats := make([]map[string]interface{}, len(mvp.variants))
	for i, v := range mvp.variants {
		variantStats[i] = map[string]interface{}{
			"index":          i,
			"bandwidth":      v.Bandwidth,
			"resolution":     v.Resolution,
			"total_segments": len(v.Segments),
			"position":       mvp.variantPos[i],
		}
	}

	return map[string]interface{}{
		"is_master":       true,
		"window_size":     mvp.windowSize,
		"sequence_number": mvp.sequenceNumber,
		"target_duration": mvp.targetDuration,
		"variants":        variantStats,
		"variant_count":   len(mvp.variants),
	}
}

// getCurrentWindowForVariant returns the current window of segments for a specific variant.
// Window size is clamped to the variant's segment count if necessary.
// Caller must hold at least a read lock.
func (mvp *multiVariantPlaylist) getCurrentWindowForVariant(position int, segments []segment.Segment) []segment.Segment {
	totalSegments := len(segments)
	effectiveWindowSize := mvp.windowSize
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
