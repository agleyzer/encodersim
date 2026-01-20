// Package playlist implements live HLS playlist generation with sliding window support.
package playlist

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/agleyzer/encodersim/internal/cluster"
	"github.com/agleyzer/encodersim/internal/segment"
	"github.com/agleyzer/encodersim/internal/variant"
)

// Playlist manages a multi-variant HLS playlist with sliding window support.
// It generates both the master playlist (with variant links) and individual variant
// media playlists. For single media playlists, wrap them in a single-variant structure.
type Playlist struct {
	variants         []variant.Variant // Metadata for master playlist generation
	variantPlaylists []*mediaPlaylist  // One mediaPlaylist per variant
	clusterMgr       *cluster.Manager  // Optional: nil for non-clustered mode
	logger           *slog.Logger
}

// New creates a new multi-variant playlist.
// For single media playlists, wrap them in a variant.Variant slice first.
// Use clusterMgr=nil for non-clustered mode.
func New(variants []variant.Variant, windowSize int, clusterMgr *cluster.Manager, logger *slog.Logger) (*Playlist, error) {
	if len(variants) == 0 {
		return nil, fmt.Errorf("cannot create playlist with zero variants")
	}

	if windowSize <= 0 {
		return nil, fmt.Errorf("window size must be positive")
	}

	// Create one mediaPlaylist per variant
	variantPlaylists := make([]*mediaPlaylist, len(variants))
	variantStates := make([]cluster.VariantState, len(variants))

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

		// Initialize variant state for cluster mode
		variantStates[i] = cluster.VariantState{
			Index:           i,
			CurrentPosition: 0,
			SequenceNumber:  0,
			TotalSegments:   len(v.Segments),
		}
	}

	// Initialize cluster state if in cluster mode
	if clusterMgr != nil && clusterMgr.IsLeader() {
		initState := cluster.ClusterState{
			Variants: variantStates,
		}
		if err := clusterMgr.Initialize(initState); err != nil {
			return nil, fmt.Errorf("initialize cluster state: %w", err)
		}
		logger.Info("initialized cluster state", "variants", len(variantStates))
	} else if clusterMgr != nil {
		logger.Info("skipping cluster state initialization (not leader)")
	}

	return &Playlist{
		variants:         variants,
		variantPlaylists: variantPlaylists,
		clusterMgr:       clusterMgr,
		logger:           logger,
	}, nil
}

// Generate creates an HLS master playlist with variant streams.
func (p *Playlist) Generate() (string, error) {
	var b strings.Builder

	// HLS master playlist header
	fmt.Fprintln(&b, "#EXTM3U")
	fmt.Fprintln(&b, "#EXT-X-VERSION:3")

	// Write variant streams
	for i, v := range p.variants {
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
func (p *Playlist) GenerateVariant(variantIndex int) (string, error) {
	if variantIndex < 0 || variantIndex >= len(p.variantPlaylists) {
		return "", fmt.Errorf("variant index %d out of range (0-%d)", variantIndex, len(p.variantPlaylists)-1)
	}

	// If in cluster mode, sync state from cluster
	if p.clusterMgr != nil {
		state := p.clusterMgr.GetState()
		if len(state.Variants) == 0 || variantIndex >= len(state.Variants) {
			return "", fmt.Errorf("cluster state not initialized for variant %d", variantIndex)
		}

		// Update variant playlist with cluster state
		mp := p.variantPlaylists[variantIndex]
		mp.mu.Lock()
		mp.currentPosition = state.Variants[variantIndex].CurrentPosition
		mp.sequenceNumber = state.Variants[variantIndex].SequenceNumber
		mp.mu.Unlock()
	}

	// Delegate to the variant's mediaPlaylist
	return p.variantPlaylists[variantIndex].generate()
}

// Advance moves the sliding window forward by one segment for all variants.
func (p *Playlist) Advance() {
	// In cluster mode, only the leader advances
	if p.clusterMgr != nil {
		if !p.clusterMgr.IsLeader() {
			return
		}
		if err := p.clusterMgr.AdvanceWindow(); err != nil {
			p.logger.Error("failed to advance window", "error", err)
		}
		return
	}

	// Non-cluster mode: advance each variant independently
	for i, mp := range p.variantPlaylists {
		mp.advance()
		if i == 0 {
			// Only log for first variant to avoid spam
			p.logger.Debug("advanced all variant windows",
				"variants", len(p.variants),
			)
		}
	}
}

// StartAutoAdvance starts a goroutine that automatically advances the window
// based on the target duration.
func (p *Playlist) StartAutoAdvance(ctx context.Context) {
	// Use maximum target duration across all variants
	maxTargetDuration := 0
	for _, mp := range p.variantPlaylists {
		if mp.targetDuration > maxTargetDuration {
			maxTargetDuration = mp.targetDuration
		}
	}

	interval := time.Duration(maxTargetDuration) * time.Second

	if p.clusterMgr != nil {
		p.logger.Info("starting cluster-aware auto-advance",
			"interval", interval,
			"variantCount", len(p.variants),
		)
	} else {
		p.logger.Info("starting auto-advance for all variants",
			"interval", interval,
			"variantCount", len(p.variants),
		)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping auto-advance")
			return
		case <-ticker.C:
			p.Advance()
		}
	}
}

// GetStats returns current statistics about the playlist.
// Includes per-variant statistics.
func (p *Playlist) GetStats() map[string]any {
	// Build per-variant stats from each mediaPlaylist
	variantStats := make([]map[string]any, len(p.variants))
	for i := range p.variants {
		v := p.variants[i]
		mp := p.variantPlaylists[i]

		// Get stats from the mediaPlaylist
		mpStats := mp.getStats()

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
	for _, mp := range p.variantPlaylists {
		if mp.targetDuration > maxTargetDuration {
			maxTargetDuration = mp.targetDuration
		}
	}

	stats := map[string]any{
		"is_master":       true,
		"window_size":     p.variantPlaylists[0].windowSize,
		"sequence_number": p.variantPlaylists[0].sequenceNumber,
		"target_duration": maxTargetDuration,
		"variants":        variantStats,
		"variant_count":   len(p.variants),
	}

	// Add cluster information if in cluster mode
	if p.clusterMgr != nil {
		state := p.clusterMgr.GetState()
		stats["cluster_mode"] = true
		stats["is_leader"] = p.clusterMgr.IsLeader()
		stats["leader_address"] = p.clusterMgr.LeaderAddr()
		stats["raft_state"] = p.clusterMgr.State()

		// Update variant stats with cluster state
		if len(state.Variants) > 0 {
			for i := range variantStats {
				if i < len(state.Variants) {
					variantStats[i]["position"] = state.Variants[i].CurrentPosition
					variantStats[i]["sequence_number"] = state.Variants[i].SequenceNumber
				}
			}
		}
	}

	return stats
}

// mediaPlaylist manages a sliding window for a single media playlist.
// This is a private helper type used internally by Playlist.
type mediaPlaylist struct {
	mu              sync.RWMutex
	segments        []segment.Segment
	windowSize      int
	currentPosition int
	sequenceNumber  uint64
	targetDuration  int
	logger          *slog.Logger
}

// generate creates an HLS media playlist for the current window.
func (mp *mediaPlaylist) generate() (string, error) {
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

// advance moves the sliding window forward by one segment.
func (mp *mediaPlaylist) advance() {
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

// getStats returns current statistics about the playlist.
func (mp *mediaPlaylist) getStats() map[string]any {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	return map[string]any{
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
