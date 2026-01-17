package playlist

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agleyzer/encodersim/internal/segment"
)

func createTestSegments(count int) []segment.Segment {
	segments := make([]segment.Segment, count)
	for i := 0; i < count; i++ {
		segments[i] = segment.Segment{
			URL:      "https://example.com/segment" + string(rune('0'+i)) + ".ts",
			Duration: 10.0,
			Sequence: i,
		}
	}
	return segments
}

func createTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only show errors in tests
	}))
}

func TestNew(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(10)

	lp, err := New(segments, 6, 10, logger)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(lp.segments) != 10 {
		t.Errorf("Expected 10 segments, got %d", len(lp.segments))
	}
	if lp.windowSize != 6 {
		t.Errorf("Expected window size 6, got %d", lp.windowSize)
	}
	if lp.targetDuration != 10 {
		t.Errorf("Expected target duration 10, got %d", lp.targetDuration)
	}
	if lp.currentPosition != 0 {
		t.Errorf("Expected initial position 0, got %d", lp.currentPosition)
	}
	if lp.sequenceNumber != 0 {
		t.Errorf("Expected initial sequence 0, got %d", lp.sequenceNumber)
	}
}

func TestNew_EmptySegments(t *testing.T) {
	logger := createTestLogger()
	_, err := New([]segment.Segment{}, 6, 10, logger)
	if err == nil {
		t.Fatal("Expected error for empty segments, got nil")
	}
}

func TestNew_InvalidWindowSize(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(10)
	_, err := New(segments, 0, 10, logger)
	if err == nil {
		t.Fatal("Expected error for zero window size, got nil")
	}
}

func TestNew_WindowLargerThanSegments(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(5)
	lp, err := New(segments, 10, 10, logger)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	// Window size should be clamped to segment count
	if lp.windowSize != 5 {
		t.Errorf("Expected window size clamped to 5, got %d", lp.windowSize)
	}
}

func TestGenerate(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	playlist := lp.Generate()

	// Check for required HLS tags
	if !strings.Contains(playlist, "#EXTM3U") {
		t.Error("Playlist missing #EXTM3U tag")
	}
	if !strings.Contains(playlist, "#EXT-X-VERSION:3") {
		t.Error("Playlist missing #EXT-X-VERSION tag")
	}
	if !strings.Contains(playlist, "#EXT-X-TARGETDURATION:10") {
		t.Error("Playlist missing #EXT-X-TARGETDURATION tag")
	}
	if !strings.Contains(playlist, "#EXT-X-MEDIA-SEQUENCE:0") {
		t.Error("Playlist missing #EXT-X-MEDIA-SEQUENCE tag")
	}

	// Check that we have 3 segments (window size)
	segmentCount := strings.Count(playlist, "#EXTINF:")
	if segmentCount != 3 {
		t.Errorf("Expected 3 segments in playlist, got %d", segmentCount)
	}

	// Should NOT have #EXT-X-ENDLIST (live stream)
	if strings.Contains(playlist, "#EXT-X-ENDLIST") {
		t.Error("Live playlist should not have #EXT-X-ENDLIST tag")
	}
}

func TestAdvance(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	initialPos := lp.currentPosition
	initialSeq := lp.sequenceNumber

	lp.Advance()

	if lp.currentPosition != initialPos+1 {
		t.Errorf("Expected position %d, got %d", initialPos+1, lp.currentPosition)
	}
	if lp.sequenceNumber != initialSeq+1 {
		t.Errorf("Expected sequence %d, got %d", initialSeq+1, lp.sequenceNumber)
	}
}

func TestAdvance_Looping(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(5)
	lp, _ := New(segments, 3, 10, logger)

	// Advance to the end and beyond
	for i := 0; i < 5; i++ {
		lp.Advance()
	}

	// Position should wrap around to 0
	if lp.currentPosition != 0 {
		t.Errorf("Expected position to loop to 0, got %d", lp.currentPosition)
	}

	// Sequence should keep incrementing
	if lp.sequenceNumber != 5 {
		t.Errorf("Expected sequence 5, got %d", lp.sequenceNumber)
	}
}

func TestGetCurrentWindow(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	window := lp.getCurrentWindow()

	if len(window) != 3 {
		t.Errorf("Expected window size 3, got %d", len(window))
	}

	// Check that we have the first 3 segments
	for i := 0; i < 3; i++ {
		if window[i].Sequence != i {
			t.Errorf("Expected segment sequence %d, got %d", i, window[i].Sequence)
		}
	}
}

func TestGetCurrentWindow_AfterAdvance(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	lp.Advance()
	lp.Advance()

	window := lp.getCurrentWindow()

	// After 2 advances, should have segments 2, 3, 4
	if len(window) != 3 {
		t.Errorf("Expected window size 3, got %d", len(window))
	}
	if window[0].Sequence != 2 {
		t.Errorf("Expected first segment sequence 2, got %d", window[0].Sequence)
	}
	if window[2].Sequence != 4 {
		t.Errorf("Expected last segment sequence 4, got %d", window[2].Sequence)
	}
}

func TestGetCurrentWindow_Looping(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(5)
	lp, _ := New(segments, 3, 10, logger)

	// Advance to position 4 (last segment)
	for i := 0; i < 4; i++ {
		lp.Advance()
	}

	window := lp.getCurrentWindow()

	// Window should wrap: segments 4, 0, 1
	if len(window) != 3 {
		t.Errorf("Expected window size 3, got %d", len(window))
	}
	if window[0].Sequence != 4 {
		t.Errorf("Expected first segment sequence 4, got %d", window[0].Sequence)
	}
	if window[1].Sequence != 0 {
		t.Errorf("Expected second segment sequence 0 (wrapped), got %d", window[1].Sequence)
	}
	if window[2].Sequence != 1 {
		t.Errorf("Expected third segment sequence 1, got %d", window[2].Sequence)
	}
}

func TestGenerate_DiscontinuityTag(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(5)
	lp, _ := New(segments, 3, 10, logger)

	// Advance to position where window will wrap
	for i := 0; i < 4; i++ {
		lp.Advance()
	}

	playlist := lp.Generate()

	// Should have discontinuity tag when looping
	if !strings.Contains(playlist, "#EXT-X-DISCONTINUITY") {
		t.Error("Expected discontinuity tag when playlist loops, not found")
	}

	// Count discontinuity tags - should have exactly 1
	count := strings.Count(playlist, "#EXT-X-DISCONTINUITY")
	if count != 1 {
		t.Errorf("Expected 1 discontinuity tag, found %d", count)
	}
}

func TestGenerate_NoDiscontinuityWhenNotLooping(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(10)
	lp, _ := New(segments, 3, 10, logger)

	// Don't advance, or advance only slightly
	lp.Advance()

	playlist := lp.Generate()

	// Should NOT have discontinuity tag when not looping
	if strings.Contains(playlist, "#EXT-X-DISCONTINUITY") {
		t.Error("Should not have discontinuity tag when not looping")
	}
}

func TestStartAutoAdvance(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(10)
	lp, _ := New(segments, 3, 1, logger) // Use 1 second interval for faster testing

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go lp.StartAutoAdvance(ctx)

	// Wait for a couple advances
	time.Sleep(2500 * time.Millisecond)

	// Should have advanced at least twice
	lp.mu.RLock()
	position := lp.currentPosition
	sequence := lp.sequenceNumber
	lp.mu.RUnlock()

	if position < 2 {
		t.Errorf("Expected position >= 2 after 2.5 seconds, got %d", position)
	}
	if sequence < 2 {
		t.Errorf("Expected sequence >= 2 after 2.5 seconds, got %d", sequence)
	}

	// Cancel context and ensure it stops
	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestGetStats(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(10)
	lp, _ := New(segments, 6, 10, logger)

	lp.Advance()
	lp.Advance()

	stats := lp.GetStats()

	if stats["total_segments"] != 10 {
		t.Errorf("Expected total_segments 10, got %v", stats["total_segments"])
	}
	if stats["window_size"] != 6 {
		t.Errorf("Expected window_size 6, got %v", stats["window_size"])
	}
	if stats["current_position"] != 2 {
		t.Errorf("Expected current_position 2, got %v", stats["current_position"])
	}
	if stats["sequence_number"] != uint64(2) {
		t.Errorf("Expected sequence_number 2, got %v", stats["sequence_number"])
	}
	if stats["target_duration"] != 10 {
		t.Errorf("Expected target_duration 10, got %v", stats["target_duration"])
	}
}

func TestConcurrentAccess(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(20)
	lp, _ := New(segments, 6, 10, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-advance
	go lp.StartAutoAdvance(ctx)

	// Concurrently generate playlists while advancing
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = lp.Generate()
				_ = lp.GetStats()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	cancel()
}
