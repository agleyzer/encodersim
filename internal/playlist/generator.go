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
	// For master playlists, returns the master playlist content.
	Generate() (string, error)

	// GenerateVariant creates an HLS media playlist for a specific variant.
	// Returns an error if called on a non-master playlist or if variant index is invalid.
	GenerateVariant(variantIndex int) (string, error)

	// Advance moves the sliding window forward by one segment.
	Advance()

	// StartAutoAdvance starts a goroutine that automatically advances the window
	// based on the target duration.
	StartAutoAdvance(ctx context.Context)

	// GetStats returns current statistics about the playlist.
	GetStats() map[string]any
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

	// Create one mediaPlaylist per variant
	variantPlaylists := make([]*mediaPlaylist, len(variants))
	for i, v := range variants {
		if len(v.Segments) == 0 {
			return nil, fmt.Errorf("variant %d has zero segments", i)
		}

		// Adjust window size if needed
		effectiveWindowSize := windowSize
		if windowSize > len(v.Segments) {
			effectiveWindowSize = len(v.Segments)
			logger.Warn("window size larger than variant segment count",
				"variant", i,
				"windowSize", windowSize,
				"segmentCount", len(v.Segments),
			)
		}

		// Create mediaPlaylist for this variant
		mp := &mediaPlaylist{
			segments:        v.Segments,
			windowSize:      effectiveWindowSize,
			currentPosition: 0,
			sequenceNumber:  0,
			targetDuration:  v.TargetDuration,
			logger:          logger,
		}
		variantPlaylists[i] = mp
	}

	return &multiVariantPlaylist{
		variants:         variants,
		variantPlaylists: variantPlaylists,
		logger:           logger,
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
	fmt.Fprintln(&b, "#EXTM3U")
	fmt.Fprintln(&b, "#EXT-X-VERSION:3")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", mp.targetDuration)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", mp.sequenceNumber)

	// Get current window of segments
	windowSegments := mp.getCurrentWindow()

	// Write segments with discontinuity detection
	for i, seg := range windowSegments {
		// Check for discontinuity (loop point)
		// If this segment's sequence is less than the previous segment's,
		// we've wrapped around to the beginning
		if i > 0 && seg.Sequence < windowSegments[i-1].Sequence {
			fmt.Fprintln(&b, "#EXT-X-DISCONTINUITY")
		}

		fmt.Fprintf(&b, "#EXTINF:%.3f,\n", seg.Duration)
		fmt.Fprintln(&b, seg.URL)
	}

	// NOTE: We do NOT include #EXT-X-ENDLIST because this is a live stream

	return b.String(), nil
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
func (mp *mediaPlaylist) GetStats() map[string]any {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return map[string]any{
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
	variants         []variant.Variant // Metadata for master playlist generation
	variantPlaylists []*mediaPlaylist  // One mediaPlaylist per variant
	logger           *slog.Logger
}

// Generate creates an HLS master playlist with variant streams.
func (mvp *multiVariantPlaylist) Generate() (string, error) {
	var b strings.Builder

	// HLS master playlist header
	fmt.Fprintln(&b, "#EXTM3U")
	fmt.Fprintln(&b, "#EXT-X-VERSION:3")

	// Write variant streams
	for i, v := range mvp.variants {
		// Build #EXT-X-STREAM-INF attributes
		fmt.Fprint(&b, "#EXT-X-STREAM-INF:")
		fmt.Fprintf(&b, "BANDWIDTH=%d", v.Bandwidth)

		if v.Resolution != "" {
			fmt.Fprintf(&b, ",RESOLUTION=%s", v.Resolution)
		}

		if v.Codecs != "" {
			fmt.Fprintf(&b, ",CODECS=\"%s\"", v.Codecs)
		}

		fmt.Fprintln(&b)

		// Write variant playlist URL
		fmt.Fprintf(&b, "/variant/%d/playlist.m3u8\n", i)
	}

	return b.String(), nil
}

// GenerateVariant creates an HLS media playlist for a specific variant.
func (mvp *multiVariantPlaylist) GenerateVariant(variantIndex int) (string, error) {
	if variantIndex < 0 || variantIndex >= len(mvp.variantPlaylists) {
		return "", fmt.Errorf("variant index %d out of range (0-%d)", variantIndex, len(mvp.variantPlaylists)-1)
	}

	// Delegate to the variant's mediaPlaylist
	return mvp.variantPlaylists[variantIndex].Generate()
}

// Advance moves the sliding window forward by one segment for all variants.
func (mvp *multiVariantPlaylist) Advance() {
	// Advance each variant independently
	for i, mp := range mvp.variantPlaylists {
		mp.Advance()
		if i == 0 {
			// Only log for first variant to avoid spam
			mvp.logger.Debug("advanced all variant windows",
				"variants", len(mvp.variants),
			)
		}
	}
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration.
// Each variant runs its own auto-advance goroutine independently.
func (mvp *multiVariantPlaylist) StartAutoAdvance(ctx context.Context) {
	mvp.logger.Info("starting auto-advance for all variants",
		"variantCount", len(mvp.variants),
	)

	// Start auto-advance for each variant playlist
	for _, mp := range mvp.variantPlaylists {
		go mp.StartAutoAdvance(ctx)
	}
}

// GetStats returns current statistics about the playlist.
// Includes per-variant statistics.
func (mvp *multiVariantPlaylist) GetStats() map[string]any {
	// Build per-variant stats from each mediaPlaylist
	variantStats := make([]map[string]any, len(mvp.variants))
	for i := range mvp.variants {
		v := mvp.variants[i]
		mp := mvp.variantPlaylists[i]

		// Get stats from the mediaPlaylist
		mpStats := mp.GetStats()

		variantStats[i] = map[string]any{
			"index":          i,
			"bandwidth":      v.Bandwidth,
			"resolution":     v.Resolution,
			"total_segments": mpStats["total_segments"],
			"position":       mpStats["current_position"],
		}
	}

	// Calculate aggregate stats
	// Use max target duration across variants
	maxTargetDuration := 0
	for _, mp := range mvp.variantPlaylists {
		stats := mp.GetStats()
		if td := stats["target_duration"].(int); td > maxTargetDuration {
			maxTargetDuration = td
		}
	}

	return map[string]any{
		"is_master":       true,
		"window_size":     mvp.variantPlaylists[0].windowSize,
		"sequence_number": mvp.variantPlaylists[0].sequenceNumber,
		"target_duration": maxTargetDuration,
		"variants":        variantStats,
		"variant_count":   len(mvp.variants),
	}
}
