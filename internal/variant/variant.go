// Package variant defines data structures for HLS variant streams in master playlists.
package variant

import "github.com/agleyzer/encodersim/internal/segment"

// Variant represents a single variant stream in an HLS master playlist.
// Each variant typically represents a different quality level (bitrate/resolution).
type Variant struct {
	// Bandwidth is the peak segment bitrate in bits per second
	Bandwidth int

	// Resolution is the video resolution (e.g., "1920x1080", "1280x720")
	// Empty string if not specified in master playlist
	Resolution string

	// Codecs is the codec string (e.g., "avc1.4d401f,mp4a.40.2")
	// Empty string if not specified in master playlist
	Codecs string

	// PlaylistURL is the URL of the variant's media playlist
	PlaylistURL string

	// Segments contains all segments from this variant's media playlist
	Segments []segment.Segment

	// TargetDuration is the maximum segment duration in seconds
	TargetDuration int
}
