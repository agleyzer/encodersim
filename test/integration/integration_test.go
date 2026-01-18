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

// TestMasterPlaylist verifies that master playlists with multiple variants work correctly.
func TestMasterPlaylist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create test harness
	harness := NewTestHarness(t)
	defer harness.Cleanup()

	// Create master playlist with 2 variants
	masterPlaylist := createTestMasterPlaylist()
	lowVariantPlaylist := createTestMediaPlaylist("low", 5, 1.0)
	highVariantPlaylist := createTestMediaPlaylist("high", 5, 1.0)

	// Start HTTP server serving the master and variant playlists
	harness.StartHTTPServer(masterPlaylist, "master.m3u8")
	harness.AddPlaylist(lowVariantPlaylist, "low.m3u8")
	harness.AddPlaylist(highVariantPlaylist, "high.m3u8")

	// Start encodersim with window size of 3
	harness.StartEncoderSim("master.m3u8", 3)

	// Test Phase 1: Verify master playlist
	t.Log("Phase 1: Verifying master playlist...")
	playlist := harness.FetchPlaylist()

	// Master playlist should contain STREAM-INF tags
	if !strings.Contains(playlist, "#EXT-X-STREAM-INF") {
		t.Error("master playlist should contain #EXT-X-STREAM-INF tag")
	}

	// Should have variant playlist URLs
	if !strings.Contains(playlist, "/variant0/playlist.m3u8") {
		t.Error("master playlist should contain variant0 URL")
	}
	if !strings.Contains(playlist, "/variant1/playlist.m3u8") {
		t.Error("master playlist should contain variant1 URL")
	}

	// Should have bandwidth info
	if !strings.Contains(playlist, "BANDWIDTH=1280000") {
		t.Error("master playlist should contain low variant bandwidth")
	}
	if !strings.Contains(playlist, "BANDWIDTH=2560000") {
		t.Error("master playlist should contain high variant bandwidth")
	}

	// Master playlist should NOT have segment URLs
	if strings.Contains(playlist, "_seg") {
		t.Error("master playlist should not contain segment URLs")
	}

	t.Log("Phase 1: Master playlist verified ✓")

	// Test Phase 2: Verify variant playlists
	t.Log("Phase 2: Verifying variant playlists...")

	// Fetch low variant playlist
	lowPlaylist := harness.FetchVariantPlaylist(0)
	parsedLow := ParsePlaylist(lowPlaylist)

	if len(parsedLow.Segments) != 3 {
		t.Errorf("expected 3 segments in low variant window, got %d", len(parsedLow.Segments))
	}

	// Should have low variant segments
	if !strings.Contains(parsedLow.Segments[0].URL, "low_seg") {
		t.Error("low variant playlist should contain low variant segments")
	}

	// Fetch high variant playlist
	highPlaylist := harness.FetchVariantPlaylist(1)
	parsedHigh := ParsePlaylist(highPlaylist)

	if len(parsedHigh.Segments) != 3 {
		t.Errorf("expected 3 segments in high variant window, got %d", len(parsedHigh.Segments))
	}

	// Should have high variant segments
	if !strings.Contains(parsedHigh.Segments[0].URL, "high_seg") {
		t.Error("high variant playlist should contain high variant segments")
	}

	t.Log("Phase 2: Variant playlists verified ✓")

	// Test Phase 3: Verify synchronized advancement
	t.Log("Phase 3: Verifying synchronized advancement...")

	initialLowSeq := parsedLow.MediaSequence
	initialHighSeq := parsedHigh.MediaSequence

	// Wait for advancement
	time.Sleep(2 * time.Second)

	lowPlaylist = harness.FetchVariantPlaylist(0)
	parsedLow = ParsePlaylist(lowPlaylist)
	highPlaylist = harness.FetchVariantPlaylist(1)
	parsedHigh = ParsePlaylist(highPlaylist)

	// Both variants should have advanced
	if parsedLow.MediaSequence <= initialLowSeq {
		t.Error("low variant should have advanced")
	}
	if parsedHigh.MediaSequence <= initialHighSeq {
		t.Error("high variant should have advanced")
	}

	// Both variants should have the same sequence number (synchronized)
	if parsedLow.MediaSequence != parsedHigh.MediaSequence {
		t.Errorf("variants should be synchronized: low=%d, high=%d",
			parsedLow.MediaSequence, parsedHigh.MediaSequence)
	}

	t.Log("Phase 3: Synchronized advancement verified ✓")

	// Test Phase 4: Verify wrapping with discontinuity in variants
	t.Log("Phase 4: Waiting for variants to wrap...")

	var foundLowDiscontinuity, foundHighDiscontinuity bool

	harness.WaitForCondition(func() bool {
		lowPlaylist := harness.FetchVariantPlaylist(0)
		parsedLow := ParsePlaylist(lowPlaylist)
		highPlaylist := harness.FetchVariantPlaylist(1)
		parsedHigh := ParsePlaylist(highPlaylist)

		for _, seg := range parsedLow.Segments {
			if seg.Discontinuity {
				foundLowDiscontinuity = true
			}
		}

		for _, seg := range parsedHigh.Segments {
			if seg.Discontinuity {
				foundHighDiscontinuity = true
			}
		}

		return foundLowDiscontinuity && foundHighDiscontinuity
	}, 15*time.Second, "variants to wrap and show discontinuity")

	if !foundLowDiscontinuity {
		t.Error("expected discontinuity in low variant when wrapping")
	}
	if !foundHighDiscontinuity {
		t.Error("expected discontinuity in high variant when wrapping")
	}

	t.Log("Phase 4: Variant wrapping with discontinuity verified ✓")

	// Test Phase 5: Verify health endpoint
	t.Log("Phase 5: Verifying health endpoint...")

	health := harness.FetchHealth()

	// Should indicate master mode
	if !strings.Contains(health, "\"is_master\":true") {
		t.Error("health endpoint should indicate master mode")
	}

	// Should have variant info
	if !strings.Contains(health, "variants") {
		t.Error("health endpoint should contain variant information")
	}

	t.Log("Phase 5: Health endpoint verified ✓")
	t.Log("✅ All phases passed!")
}

// createTestMasterPlaylist creates a test HLS master playlist.
func createTestMasterPlaylist() string {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=640x360,CODECS=\"avc1.4d401e,mp4a.40.2\"\n")
	sb.WriteString("low.m3u8\n")
	sb.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1280x720,CODECS=\"avc1.4d401f,mp4a.40.2\"\n")
	sb.WriteString("high.m3u8\n")

	return sb.String()
}

// createTestMediaPlaylist creates a test HLS media playlist with a variant prefix.
func createTestMediaPlaylist(variant string, numSegments int, duration float64) string {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:1\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")

	for i := 0; i < numSegments; i++ {
		sb.WriteString("#EXTINF:1.000,\n")
		sb.WriteString("https://example.com/")
		sb.WriteString(variant)
		sb.WriteString("_seg")
		sb.WriteString(padNumber(i, 3))
		sb.WriteString(".ts\n")
	}

	sb.WriteString("#EXT-X-ENDLIST\n")

	return sb.String()
}
