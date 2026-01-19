package playlist

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agleyzer/encodersim/internal/segment"
	"github.com/agleyzer/encodersim/internal/variant"
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

	stats := lp.GetStats()
	if stats["total_segments"].(int) != 10 {
		t.Errorf("Expected 10 segments, got %d", stats["total_segments"])
	}
	if stats["window_size"].(int) != 6 {
		t.Errorf("Expected window size 6, got %d", stats["window_size"])
	}
	if stats["target_duration"].(int) != 10 {
		t.Errorf("Expected target duration 10, got %d", stats["target_duration"])
	}
	if stats["current_position"].(int) != 0 {
		t.Errorf("Expected initial position 0, got %d", stats["current_position"])
	}
	if stats["sequence_number"].(uint64) != 0 {
		t.Errorf("Expected initial sequence 0, got %d", stats["sequence_number"])
	}
	if stats["is_master"].(bool) != false {
		t.Errorf("Expected is_master to be false")
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
	stats := lp.GetStats()
	if stats["window_size"].(int) != 5 {
		t.Errorf("Expected window size clamped to 5, got %d", stats["window_size"])
	}
}

func TestGenerate(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	playlist, err := lp.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

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

	stats := lp.GetStats()
	initialPos := stats["current_position"].(int)
	initialSeq := stats["sequence_number"].(uint64)

	lp.Advance()

	stats = lp.GetStats()
	if stats["current_position"].(int) != initialPos+1 {
		t.Errorf("Expected position %d, got %d", initialPos+1, stats["current_position"])
	}
	if stats["sequence_number"].(uint64) != initialSeq+1 {
		t.Errorf("Expected sequence %d, got %d", initialSeq+1, stats["sequence_number"])
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
	stats := lp.GetStats()
	if stats["current_position"].(int) != 0 {
		t.Errorf("Expected position to loop to 0, got %d", stats["current_position"])
	}

	// Sequence should keep incrementing
	if stats["sequence_number"].(uint64) != 5 {
		t.Errorf("Expected sequence 5, got %d", stats["sequence_number"])
	}
}

// TestGetCurrentWindow removed - getCurrentWindow() is now a private method.
// This functionality is tested through the public Generate() method in other tests.

// TestGetCurrentWindow_AfterAdvance removed - getCurrentWindow() is now a private method.
// This functionality is tested through the public Generate() method in other tests.

// TestGetCurrentWindow_Looping removed - getCurrentWindow() is now a private method.
// This functionality is tested through the public Generate() method in TestGenerateWithDiscontinuity.

func TestGenerate_DiscontinuityTag(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(5)
	lp, _ := New(segments, 3, 10, logger)

	// Advance to position where window will wrap
	for i := 0; i < 4; i++ {
		lp.Advance()
	}

	playlist, err := lp.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

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

	playlist, err := lp.Generate()
	if err != nil {
		t.Fatalf("Generate() returned error: %v", err)
	}

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
	stats := lp.GetStats()
	position := stats["current_position"].(int)
	sequence := stats["sequence_number"].(uint64)

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
				_, _ = lp.Generate()
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

// Helper function to create test variants
func createTestVariants(count int, segmentsPerVariant int) []variant.Variant {
	variants := make([]variant.Variant, count)
	for i := 0; i < count; i++ {
		segments := make([]segment.Segment, segmentsPerVariant)
		for j := 0; j < segmentsPerVariant; j++ {
			segments[j] = segment.Segment{
				URL:          "https://example.com/v" + string(rune('0'+i)) + "_seg" + string(rune('0'+j)) + ".ts",
				Duration:     10.0,
				Sequence:     j,
				VariantIndex: i,
			}
		}
		variants[i] = variant.Variant{
			Bandwidth:      1000000 * (i + 1),
			Resolution:     "1280x720",
			Codecs:         "avc1.4d401f,mp4a.40.2",
			PlaylistURL:    "https://example.com/variant" + string(rune('0'+i)) + ".m3u8",
			Segments:       segments,
			TargetDuration: 10,
		}
	}
	return variants
}

func TestNewMaster(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(3, 10)

	lp, err := NewMaster(variants, 6, logger)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	stats := lp.GetStats()
	if !stats["is_master"].(bool) {
		t.Error("Expected is_master to be true")
	}
	if stats["variant_count"].(int) != 3 {
		t.Errorf("Expected 3 variants, got %d", stats["variant_count"])
	}
	if stats["window_size"].(int) != 6 {
		t.Errorf("Expected window size 6, got %d", stats["window_size"])
	}
	if stats["target_duration"].(int) != 10 {
		t.Errorf("Expected target duration 10, got %d", stats["target_duration"])
	}
	variantStats := stats["variants"].([]map[string]interface{})
	if len(variantStats) != 3 {
		t.Errorf("Expected 3 variant positions, got %d", len(variantStats))
	}
	// All positions should start at 0
	for i, vs := range variantStats {
		if vs["position"].(int) != 0 {
			t.Errorf("Expected variant %d position 0, got %d", i, vs["position"])
		}
	}
}

func TestNewMaster_EmptyVariants(t *testing.T) {
	logger := createTestLogger()
	_, err := NewMaster([]variant.Variant{}, 6, logger)
	if err == nil {
		t.Fatal("Expected error for empty variants, got nil")
	}
}

func TestNewMaster_InvalidWindowSize(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 10)
	_, err := NewMaster(variants, 0, logger)
	if err == nil {
		t.Fatal("Expected error for zero window size, got nil")
	}
}

func TestNewMaster_MaxTargetDuration(t *testing.T) {
	logger := createTestLogger()
	variants := []variant.Variant{
		{
			Bandwidth:      1000000,
			Segments:       createTestSegments(5),
			TargetDuration: 8,
		},
		{
			Bandwidth:      2000000,
			Segments:       createTestSegments(5),
			TargetDuration: 12,
		},
		{
			Bandwidth:      3000000,
			Segments:       createTestSegments(5),
			TargetDuration: 10,
		},
	}

	lp, err := NewMaster(variants, 3, logger)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should use max target duration across all variants
	stats := lp.GetStats()
	if stats["target_duration"].(int) != 12 {
		t.Errorf("Expected target duration 12 (max across variants), got %d", stats["target_duration"])
	}
}

func TestGenerateMaster(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 10)
	lp, _ := NewMaster(variants, 6, logger)

	playlist, err := lp.GenerateMaster()
	if err != nil {
		t.Fatalf("GenerateMaster() returned error: %v", err)
	}

	// Check for required master playlist tags
	if !strings.Contains(playlist, "#EXTM3U") {
		t.Error("Master playlist missing #EXTM3U tag")
	}
	if !strings.Contains(playlist, "#EXT-X-VERSION:3") {
		t.Error("Master playlist missing #EXT-X-VERSION tag")
	}

	// Check for variant stream info
	if !strings.Contains(playlist, "#EXT-X-STREAM-INF:") {
		t.Error("Master playlist missing #EXT-X-STREAM-INF tag")
	}
	if !strings.Contains(playlist, "BANDWIDTH=1000000") {
		t.Error("Master playlist missing first variant bandwidth")
	}
	if !strings.Contains(playlist, "BANDWIDTH=2000000") {
		t.Error("Master playlist missing second variant bandwidth")
	}

	// Check for variant playlist URLs
	if !strings.Contains(playlist, "/variant0/playlist.m3u8") {
		t.Error("Master playlist missing variant 0 URL")
	}
	if !strings.Contains(playlist, "/variant1/playlist.m3u8") {
		t.Error("Master playlist missing variant 1 URL")
	}

	// Should NOT have segment URLs (only in variant playlists)
	if strings.Contains(playlist, ".ts") {
		t.Error("Master playlist should not contain segment URLs")
	}
}

func TestGenerateVariant(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 8)
	lp, _ := NewMaster(variants, 3, logger)

	// Generate first variant
	playlist, err := lp.GenerateVariant(0)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Check for required media playlist tags
	if !strings.Contains(playlist, "#EXTM3U") {
		t.Error("Variant playlist missing #EXTM3U tag")
	}
	if !strings.Contains(playlist, "#EXT-X-VERSION:3") {
		t.Error("Variant playlist missing #EXT-X-VERSION tag")
	}
	if !strings.Contains(playlist, "#EXT-X-TARGETDURATION:10") {
		t.Error("Variant playlist missing #EXT-X-TARGETDURATION tag")
	}
	if !strings.Contains(playlist, "#EXT-X-MEDIA-SEQUENCE:0") {
		t.Error("Variant playlist missing #EXT-X-MEDIA-SEQUENCE tag")
	}

	// Check segment count matches window size
	segmentCount := strings.Count(playlist, "#EXTINF:")
	if segmentCount != 3 {
		t.Errorf("Expected 3 segments in variant playlist, got %d", segmentCount)
	}

	// Should have segments from variant 0
	if !strings.Contains(playlist, "v0_seg") {
		t.Error("Variant 0 playlist should contain variant 0 segments")
	}
}

func TestGenerateVariant_InvalidIndex(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 8)
	lp, _ := NewMaster(variants, 3, logger)

	// Try invalid indices
	_, err := lp.GenerateVariant(-1)
	if err == nil {
		t.Error("Expected error for negative variant index, got nil")
	}

	_, err = lp.GenerateVariant(2)
	if err == nil {
		t.Error("Expected error for out-of-range variant index, got nil")
	}
}

func TestGenerateVariant_OnMediaPlaylist(t *testing.T) {
	logger := createTestLogger()
	segments := createTestSegments(8)
	lp, _ := New(segments, 3, 10, logger)

	// Should error when called on non-master playlist
	_, err := lp.GenerateVariant(0)
	if err == nil {
		t.Fatal("Expected error when calling GenerateVariant on media playlist, got nil")
	}
}

func TestAdvance_MultiVariant(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(3, 10)
	lp, _ := NewMaster(variants, 3, logger)

	stats := lp.GetStats()
	initialSeq := stats["sequence_number"].(uint64)

	// Advance once
	lp.Advance()

	// All variants should advance together
	stats = lp.GetStats()
	variantStats := stats["variants"].([]map[string]interface{})
	for i, vs := range variantStats {
		if vs["position"].(int) != 1 {
			t.Errorf("Expected variant %d position 1 after advance, got %d", i, vs["position"])
		}
	}

	// Sequence should increment
	if stats["sequence_number"].(uint64) != initialSeq+1 {
		t.Errorf("Expected sequence %d, got %d", initialSeq+1, stats["sequence_number"])
	}
}

func TestAdvance_MultiVariant_Looping(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 5)
	lp, _ := NewMaster(variants, 3, logger)

	// Advance past the end
	for i := 0; i < 5; i++ {
		lp.Advance()
	}

	// All variants should wrap to position 0
	stats := lp.GetStats()
	variantStats := stats["variants"].([]map[string]interface{})
	for i, vs := range variantStats {
		if vs["position"].(int) != 0 {
			t.Errorf("Expected variant %d position to loop to 0, got %d", i, vs["position"])
		}
	}

	// Sequence should keep incrementing
	if stats["sequence_number"].(uint64) != 5 {
		t.Errorf("Expected sequence 5, got %d", stats["sequence_number"])
	}
}

func TestGetStats_MultiVariant(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(3, 10)
	lp, _ := NewMaster(variants, 6, logger)

	lp.Advance()
	lp.Advance()

	stats := lp.GetStats()

	// Check master playlist stats
	if isMaster, ok := stats["is_master"].(bool); !ok || !isMaster {
		t.Error("Expected is_master to be true")
	}
	if stats["window_size"] != 6 {
		t.Errorf("Expected window_size 6, got %v", stats["window_size"])
	}
	if stats["sequence_number"] != uint64(2) {
		t.Errorf("Expected sequence_number 2, got %v", stats["sequence_number"])
	}
	if stats["target_duration"] != 10 {
		t.Errorf("Expected target_duration 10, got %v", stats["target_duration"])
	}
	if stats["variant_count"] != 3 {
		t.Errorf("Expected variant_count 3, got %v", stats["variant_count"])
	}

	// Check per-variant stats
	variantStats, ok := stats["variants"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected variants array in stats")
	}
	if len(variantStats) != 3 {
		t.Errorf("Expected 3 variant stats, got %d", len(variantStats))
	}

	// Check first variant stats
	v0 := variantStats[0]
	if v0["index"] != 0 {
		t.Errorf("Expected variant 0 index 0, got %v", v0["index"])
	}
	if v0["bandwidth"] != 1000000 {
		t.Errorf("Expected variant 0 bandwidth 1000000, got %v", v0["bandwidth"])
	}
	if v0["total_segments"] != 10 {
		t.Errorf("Expected variant 0 total_segments 10, got %v", v0["total_segments"])
	}
	if v0["position"] != 2 {
		t.Errorf("Expected variant 0 position 2, got %v", v0["position"])
	}
}

func TestGenerateVariant_DiscontinuityTag(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(2, 5)
	lp, _ := NewMaster(variants, 3, logger)

	// Advance to position where window will wrap
	for i := 0; i < 4; i++ {
		lp.Advance()
	}

	// Generate variant playlist
	playlist, err := lp.GenerateVariant(0)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should have discontinuity tag when looping
	if !strings.Contains(playlist, "#EXT-X-DISCONTINUITY") {
		t.Error("Expected discontinuity tag when variant playlist loops, not found")
	}

	// Count discontinuity tags - should have exactly 1
	count := strings.Count(playlist, "#EXT-X-DISCONTINUITY")
	if count != 1 {
		t.Errorf("Expected 1 discontinuity tag, found %d", count)
	}
}

func TestStartAutoAdvance_MultiVariant(t *testing.T) {
	logger := createTestLogger()
	// Create variants with 1 second target duration for faster testing
	variants := createTestVariants(3, 10)
	for i := range variants {
		variants[i].TargetDuration = 1
	}
	lp, _ := NewMaster(variants, 3, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go lp.StartAutoAdvance(ctx)

	// Wait for a couple advances
	time.Sleep(2500 * time.Millisecond)

	// All variants should have advanced
	stats := lp.GetStats()
	variantStats := stats["variants"].([]map[string]interface{})
	for i, vs := range variantStats {
		if vs["position"].(int) < 2 {
			t.Errorf("Expected variant %d position >= 2 after 2.5 seconds, got %d", i, vs["position"])
		}
	}
	sequence := stats["sequence_number"].(uint64)

	if sequence < 2 {
		t.Errorf("Expected sequence >= 2 after 2.5 seconds, got %d", sequence)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestConcurrentAccess_MultiVariant(t *testing.T) {
	logger := createTestLogger()
	variants := createTestVariants(3, 20)
	lp, _ := NewMaster(variants, 6, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-advance
	go lp.StartAutoAdvance(ctx)

	// Concurrently generate playlists while advancing
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_, _ = lp.GenerateMaster()
				for k := 0; k < 3; k++ {
					_, _ = lp.GenerateVariant(k)
				}
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
