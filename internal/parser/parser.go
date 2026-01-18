// Package parser provides HLS playlist parsing functionality.
package parser

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/agleyzer/encodersim/internal/segment"
	"github.com/agleyzer/encodersim/internal/variant"
	"github.com/grafov/m3u8"
)

// PlaylistInfo contains the parsed playlist information.
// Supports both master playlists (with multiple variants) and media playlists (single variant).
type PlaylistInfo struct {
	// IsMaster indicates whether this is a master playlist with multiple variants
	IsMaster bool

	// Variants contains the variant streams (only populated for master playlists)
	Variants []variant.Variant

	// Segments contains segments for a single media playlist (only populated for media playlists)
	// Kept for backward compatibility with single media playlist mode
	Segments []segment.Segment

	// TargetDuration is the maximum segment duration in seconds
	// For master playlists, this is the max across all variants
	TargetDuration int
}

// ParsePlaylist fetches and parses an HLS playlist from a URL.
func ParsePlaylist(playlistURL string) (*PlaylistInfo, error) {
	// Fetch the playlist
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(playlistURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch playlist: HTTP %d", resp.StatusCode)
	}

	// Parse the playlist
	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, fmt.Errorf("failed to parse playlist: %w", err)
	}

	// Detect playlist type and handle accordingly
	if listType == m3u8.MASTER {
		return parseMasterPlaylist(playlist, playlistURL)
	}

	// Handle media playlist
	mediaPlaylist, ok := playlist.(*m3u8.MediaPlaylist)
	if !ok {
		return nil, fmt.Errorf("unexpected playlist type")
	}

	// Extract segments
	var segments []segment.Segment
	for i, seg := range mediaPlaylist.Segments {
		if seg == nil {
			break
		}

		// Resolve segment URL to absolute
		segmentURL, err := resolveURL(playlistURL, seg.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve segment URL: %w", err)
		}

		segments = append(segments, segment.Segment{
			URL:      segmentURL,
			Duration: seg.Duration,
			Sequence: i,
		})
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("playlist contains no segments")
	}

	targetDuration := int(mediaPlaylist.TargetDuration)
	if targetDuration == 0 {
		// If target duration is not set, use the max segment duration
		maxDuration := 0.0
		for _, seg := range segments {
			if seg.Duration > maxDuration {
				maxDuration = seg.Duration
			}
		}
		targetDuration = int(maxDuration) + 1
	}

	return &PlaylistInfo{
		IsMaster:       false,
		Segments:       segments,
		TargetDuration: targetDuration,
	}, nil
}

// parseMasterPlaylist parses a master playlist and extracts variant information.
func parseMasterPlaylist(playlist m3u8.Playlist, masterURL string) (*PlaylistInfo, error) {
	masterPlaylist, ok := playlist.(*m3u8.MasterPlaylist)
	if !ok {
		return nil, fmt.Errorf("unexpected playlist type")
	}

	if len(masterPlaylist.Variants) == 0 {
		return nil, fmt.Errorf("master playlist contains no variants")
	}

	// Extract variant information and fetch each variant's media playlist
	var variants []variant.Variant
	maxTargetDuration := 0

	for variantIndex, v := range masterPlaylist.Variants {
		if v == nil {
			continue
		}

		// Resolve variant playlist URL to absolute
		variantURL, err := resolveURL(masterURL, v.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve variant URL: %w", err)
		}

		// Extract resolution if available
		resolution := ""
		if v.Resolution != "" {
			resolution = v.Resolution
		}

		// Extract codecs if available
		codecs := ""
		if v.Codecs != "" {
			codecs = v.Codecs
		}

		// Fetch and parse the variant's media playlist
		segments, targetDuration, err := parseMediaPlaylistFromURL(variantURL, variantIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to parse variant %d media playlist: %w", variantIndex, err)
		}

		// Track maximum target duration across all variants
		if targetDuration > maxTargetDuration {
			maxTargetDuration = targetDuration
		}

		variants = append(variants, variant.Variant{
			Bandwidth:      int(v.Bandwidth),
			Resolution:     resolution,
			Codecs:         codecs,
			PlaylistURL:    variantURL,
			Segments:       segments,
			TargetDuration: targetDuration,
		})
	}

	return &PlaylistInfo{
		IsMaster:       true,
		Variants:       variants,
		TargetDuration: maxTargetDuration,
	}, nil
}

// parseMediaPlaylistFromURL fetches and parses a media playlist from a URL.
func parseMediaPlaylistFromURL(playlistURL string, variantIndex int) ([]segment.Segment, int, error) {
	// Fetch the playlist
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(playlistURL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("failed to fetch playlist: HTTP %d", resp.StatusCode)
	}

	// Parse the playlist
	playlist, listType, err := m3u8.DecodeFrom(resp.Body, true)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse playlist: %w", err)
	}

	// Ensure it's a media playlist
	if listType != m3u8.MEDIA {
		return nil, 0, fmt.Errorf("expected media playlist, got master playlist")
	}

	mediaPlaylist, ok := playlist.(*m3u8.MediaPlaylist)
	if !ok {
		return nil, 0, fmt.Errorf("unexpected playlist type")
	}

	// Extract segments
	var segments []segment.Segment
	for i, seg := range mediaPlaylist.Segments {
		if seg == nil {
			break
		}

		// Resolve segment URL to absolute
		segmentURL, err := resolveURL(playlistURL, seg.URI)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to resolve segment URL: %w", err)
		}

		segments = append(segments, segment.Segment{
			URL:          segmentURL,
			Duration:     seg.Duration,
			Sequence:     i,
			VariantIndex: variantIndex,
		})
	}

	if len(segments) == 0 {
		return nil, 0, fmt.Errorf("playlist contains no segments")
	}

	targetDuration := int(mediaPlaylist.TargetDuration)
	if targetDuration == 0 {
		// If target duration is not set, use the max segment duration
		maxDuration := 0.0
		for _, seg := range segments {
			if seg.Duration > maxDuration {
				maxDuration = seg.Duration
			}
		}
		targetDuration = int(maxDuration) + 1
	}

	return segments, targetDuration, nil
}

// resolveURL resolves a possibly relative URL against a base URL.
func resolveURL(baseURL, relativeURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}

	rel, err := url.Parse(relativeURL)
	if err != nil {
		return "", fmt.Errorf("invalid relative URL: %w", err)
	}

	// Resolve the relative URL against the base
	resolved := base.ResolveReference(rel)
	return resolved.String(), nil
}

// FetchContent fetches content from a URL (helper for testing).
func FetchContent(url string) (io.ReadCloser, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return resp.Body, nil
}
