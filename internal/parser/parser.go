package parser

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/agleyzer/encodersim/pkg/segment"
	"github.com/grafov/m3u8"
)

// PlaylistInfo contains the parsed playlist information
type PlaylistInfo struct {
	Segments       []segment.Segment
	TargetDuration int
}

// ParsePlaylist fetches and parses an HLS playlist from a URL
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

	// We only support media playlists, not master playlists
	if listType != m3u8.MEDIA {
		return nil, fmt.Errorf("expected media playlist, got master playlist")
	}

	mediaPlaylist := playlist.(*m3u8.MediaPlaylist)

	// Extract segments
	segments := make([]segment.Segment, 0)
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
		Segments:       segments,
		TargetDuration: targetDuration,
	}, nil
}

// resolveURL resolves a possibly relative URL against a base URL
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

// FetchContent fetches content from a URL (helper for testing)
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
