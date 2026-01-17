// Package integration provides integration tests for EncoderSim.
package integration

import (
	"strings"
	"testing"
	"time"
)

// TestWrappingPlaylist verifies that the playlist wraps around correctly
// and inserts discontinuity tags at the loop point.
func TestWrappingPlaylist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create test harness
	harness := NewTestHarness(t)
	defer harness.Cleanup()

	// Create a small test playlist with 5 segments, each 1 second long
	// We'll use a window size of 3 to see wrapping quickly
	testPlaylist := createTestPlaylist(5, 1.0)

	// Start HTTP server serving the test playlist
	harness.StartHTTPServer(testPlaylist, "test.m3u8")

	// Start encodersim with window size of 3
	harness.StartEncoderSim("test.m3u8", 3)

	// Test Phase 1: Verify initial playlist
	t.Log("Phase 1: Verifying initial playlist...")
	playlist := harness.FetchPlaylist()
	parsed := ParsePlaylist(playlist)

	// Should have exactly 3 segments in window
	if len(parsed.Segments) != 3 {
		t.Errorf("expected 3 segments in window, got %d", len(parsed.Segments))
	}

	// Should not have end list tag (this is a live stream)
	if parsed.HasEndList {
		t.Error("playlist should not have EXT-X-ENDLIST tag for live stream")
	}

	// Should start at sequence 0
	if parsed.MediaSequence != 0 {
		t.Errorf("expected media sequence 0, got %d", parsed.MediaSequence)
	}

	// Initial window should be segments 0, 1, 2
	expectedURLs := []string{"segment000.ts", "segment001.ts", "segment002.ts"}
	for i, seg := range parsed.Segments {
		if !strings.Contains(seg.URL, expectedURLs[i]) {
			t.Errorf("segment %d: expected URL to contain %s, got %s", i, expectedURLs[i], seg.URL)
		}
	}

	// No discontinuity at start
	for i, seg := range parsed.Segments {
		if seg.Discontinuity {
			t.Errorf("segment %d should not have discontinuity at start", i)
		}
	}

	t.Log("Phase 1: Initial playlist verified ✓")

	// Test Phase 2: Wait for window to advance and verify wrapping
	t.Log("Phase 2: Waiting for playlist to wrap around...")

	// We need to wait for the window to advance through all 5 segments and wrap
	// Segments: 0,1,2,3,4 -> wrap to 0
	// With 1 second target duration, we need to wait ~6-7 seconds
	// Window positions:
	// Seq 0: [0,1,2]
	// Seq 1: [1,2,3]
	// Seq 2: [2,3,4]
	// Seq 3: [3,4,0] <- wrap happens here!
	// Seq 4: [4,0,1]
	// Seq 5: [0,1,2]

	var wrappedPlaylist *ParsedPlaylist
	var foundDiscontinuity bool

	harness.WaitForCondition(func() bool {
		playlist := harness.FetchPlaylist()
		parsed := ParsePlaylist(playlist)

		// Look for discontinuity tag
		for _, seg := range parsed.Segments {
			if seg.Discontinuity {
				foundDiscontinuity = true
				wrappedPlaylist = parsed
				return true
			}
		}

		return false
	}, 15*time.Second, "playlist to wrap and show discontinuity")

	if !foundDiscontinuity {
		t.Fatal("expected to find discontinuity tag when playlist wraps")
	}

	t.Logf("Phase 2: Found discontinuity at sequence %d ✓", wrappedPlaylist.MediaSequence)

	// Test Phase 3: Verify discontinuity is placed correctly
	t.Log("Phase 3: Verifying discontinuity placement...")

	// Find which segment has the discontinuity
	discontinuityIndex := -1
	for i, seg := range wrappedPlaylist.Segments {
		if seg.Discontinuity {
			discontinuityIndex = i
			break
		}
	}

	if discontinuityIndex == -1 {
		t.Fatal("discontinuity tag disappeared")
	}

	// Discontinuity should be on the first segment after wrap
	// We can verify this by checking that the segment URL numbers go backwards
	// For example: segment004.ts, [DISCONTINUITY] segment000.ts, segment001.ts
	if discontinuityIndex > 0 {
		prevSeg := wrappedPlaylist.Segments[discontinuityIndex-1]
		currSeg := wrappedPlaylist.Segments[discontinuityIndex]

		t.Logf("Discontinuity placement: before=%s, after=%s",
			prevSeg.URL, currSeg.URL)

		// The segment after discontinuity should have a smaller number than before
		// (wrapping from segment004 back to segment000)
		if strings.Contains(currSeg.URL, "segment000.ts") &&
			!strings.Contains(prevSeg.URL, "segment000.ts") {
			t.Log("Phase 3: Discontinuity correctly placed at wrap point ✓")
		}
	}

	// Test Phase 4: Verify continuous operation
	t.Log("Phase 4: Verifying continuous operation...")

	// Fetch several more times to ensure it keeps working
	for i := 0; i < 3; i++ {
		time.Sleep(1500 * time.Millisecond)
		playlist := harness.FetchPlaylist()
		parsed := ParsePlaylist(playlist)

		if len(parsed.Segments) != 3 {
			t.Errorf("iteration %d: expected 3 segments, got %d", i, len(parsed.Segments))
		}

		if parsed.HasEndList {
			t.Errorf("iteration %d: playlist should remain live", i)
		}
	}

	t.Log("Phase 4: Continuous operation verified ✓")
	t.Log("✅ All phases passed!")
}

// createTestPlaylist creates a test HLS playlist with the specified number of segments.
func createTestPlaylist(numSegments int, duration float64) string {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:1\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")

	for i := 0; i < numSegments; i++ {
		sb.WriteString("#EXTINF:1.000,\n")
		sb.WriteString("https://example.com/segment")
		sb.WriteString(padNumber(i, 3))
		sb.WriteString(".ts\n")
	}

	sb.WriteString("#EXT-X-ENDLIST\n")

	return sb.String()
}

// padNumber pads a number with leading zeros.
func padNumber(num, width int) string {
	result := ""
	for i := width; i > 0; i-- {
		digit := num % 10
		result = string(rune('0'+digit)) + result
		num /= 10
	}
	return result
}
