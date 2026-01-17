// Package segment defines data structures for HLS video segments.
package segment

// Segment represents a single HLS video segment.
type Segment struct {
	// URL is the original segment URL (kept as-is from the source playlist)
	URL string

	// Duration is the segment duration in seconds
	Duration float64

	// Sequence is the position in the original playlist
	Sequence int
}
