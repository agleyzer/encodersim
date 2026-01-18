package parser

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePlaylist_ValidPlaylist(t *testing.T) {
	// Create a test HTTP server with a valid playlist
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:9.9,
segment001.ts
#EXTINF:10.0,
segment002.ts
#EXTINF:10.1,
segment003.ts
#EXT-X-ENDLIST
`
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	info, err := ParsePlaylist(server.URL)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(info.Segments) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(info.Segments))
	}

	if info.TargetDuration != 11 {
		t.Errorf("Expected target duration 11, got %d", info.TargetDuration)
	}

	// Check first segment
	if info.Segments[0].Duration != 9.9 {
		t.Errorf("Expected first segment duration 9.9, got %f", info.Segments[0].Duration)
	}
	if info.Segments[0].Sequence != 0 {
		t.Errorf("Expected first segment sequence 0, got %d", info.Segments[0].Sequence)
	}

	// Check URL resolution - relative URL should be resolved to absolute
	expectedURL := server.URL + "/segment001.ts"
	if info.Segments[0].URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, info.Segments[0].URL)
	}
}

func TestParsePlaylist_AbsoluteURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:5
#EXTINF:4.0,
https://example.com/segment001.ts
#EXTINF:4.0,
https://example.com/segment002.ts
#EXT-X-ENDLIST
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	info, err := ParsePlaylist(server.URL)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Absolute URLs should remain unchanged
	if info.Segments[0].URL != "https://example.com/segment001.ts" {
		t.Errorf("Expected absolute URL unchanged, got %s", info.Segments[0].URL)
	}
}

func TestParsePlaylist_NoTargetDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXTINF:5.5,
segment001.ts
#EXTINF:8.2,
segment002.ts
#EXT-X-ENDLIST
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	info, err := ParsePlaylist(server.URL)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should calculate target duration from max segment duration
	if info.TargetDuration < 8 {
		t.Errorf("Expected target duration >= 8 (max segment duration), got %d", info.TargetDuration)
	}
}

func TestParsePlaylist_EmptyPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-ENDLIST
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error for empty playlist, got nil")
	}
}

func TestParsePlaylist_InvalidURL(t *testing.T) {
	_, err := ParsePlaylist("not-a-valid-url")
	if err == nil {
		t.Fatal("Expected error for invalid URL, got nil")
	}
}

func TestParsePlaylist_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error for HTTP 404, got nil")
	}
}

func TestParsePlaylist_MasterPlaylist(t *testing.T) {
	// Create a test HTTP server with master playlist and variant media playlists
	variantRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/" || r.URL.Path == "/master.m3u8" {
			// Master playlist
			playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=640x360,CODECS="avc1.4d401e,mp4a.40.2"
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1280x720,CODECS="avc1.4d401f,mp4a.40.2"
high.m3u8
`
			w.Write([]byte(playlist))
		} else if r.URL.Path == "/low.m3u8" {
			// Low variant media playlist
			variantRequests++
			playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
segment_low_001.ts
#EXTINF:10.0,
segment_low_002.ts
#EXT-X-ENDLIST
`
			w.Write([]byte(playlist))
		} else if r.URL.Path == "/high.m3u8" {
			// High variant media playlist
			variantRequests++
			playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
segment_high_001.ts
#EXTINF:10.0,
segment_high_002.ts
#EXT-X-ENDLIST
`
			w.Write([]byte(playlist))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	info, err := ParsePlaylist(server.URL + "/master.m3u8")
	if err != nil {
		t.Fatalf("Expected no error for master playlist, got %v", err)
	}

	// Verify master playlist properties
	if !info.IsMaster {
		t.Error("Expected IsMaster to be true")
	}

	if len(info.Variants) != 2 {
		t.Fatalf("Expected 2 variants, got %d", len(info.Variants))
	}

	// Verify low variant
	lowVariant := info.Variants[0]
	if lowVariant.Bandwidth != 1280000 {
		t.Errorf("Expected low variant bandwidth 1280000, got %d", lowVariant.Bandwidth)
	}
	if lowVariant.Resolution != "640x360" {
		t.Errorf("Expected low variant resolution '640x360', got '%s'", lowVariant.Resolution)
	}
	if lowVariant.Codecs != "avc1.4d401e,mp4a.40.2" {
		t.Errorf("Expected low variant codecs, got '%s'", lowVariant.Codecs)
	}
	if len(lowVariant.Segments) != 2 {
		t.Errorf("Expected low variant to have 2 segments, got %d", len(lowVariant.Segments))
	}
	if lowVariant.TargetDuration != 10 {
		t.Errorf("Expected low variant target duration 10, got %d", lowVariant.TargetDuration)
	}

	// Verify high variant
	highVariant := info.Variants[1]
	if highVariant.Bandwidth != 2560000 {
		t.Errorf("Expected high variant bandwidth 2560000, got %d", highVariant.Bandwidth)
	}
	if highVariant.Resolution != "1280x720" {
		t.Errorf("Expected high variant resolution '1280x720', got '%s'", highVariant.Resolution)
	}
	if len(highVariant.Segments) != 2 {
		t.Errorf("Expected high variant to have 2 segments, got %d", len(highVariant.Segments))
	}

	// Verify segments have variant index set
	if lowVariant.Segments[0].VariantIndex != 0 {
		t.Errorf("Expected low variant segment to have VariantIndex 0, got %d", lowVariant.Segments[0].VariantIndex)
	}
	if highVariant.Segments[0].VariantIndex != 1 {
		t.Errorf("Expected high variant segment to have VariantIndex 1, got %d", highVariant.Segments[0].VariantIndex)
	}

	// Verify target duration is max across variants
	if info.TargetDuration != 10 {
		t.Errorf("Expected master playlist target duration 10, got %d", info.TargetDuration)
	}

	// Verify both variant playlists were fetched
	if variantRequests != 2 {
		t.Errorf("Expected 2 variant playlist requests, got %d", variantRequests)
	}
}

func TestParsePlaylist_MasterPlaylist_NoVariants(t *testing.T) {
	// Note: An empty master playlist will be parsed as a media playlist by m3u8 library
	// and will fail with "playlist contains no segments"
	// This test verifies proper error handling for edge cases
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-VERSION:3
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error for empty playlist, got nil")
	}
	// Accept either error message since the m3u8 library may parse this as media playlist
	if err.Error() != "master playlist contains no variants" && err.Error() != "playlist contains no segments" {
		t.Errorf("Expected error about no variants or no segments, got: %v", err)
	}
}

func TestParsePlaylist_MasterPlaylist_VariantFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000
variant.m3u8
`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(playlist))
		} else {
			// Variant returns error
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error when variant fetch fails, got nil")
	}
}

func TestParsePlaylist_MasterPlaylist_RelativeURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)

		if r.URL.Path == "/playlists/master.m3u8" {
			playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000
variants/low.m3u8
`
			w.Write([]byte(playlist))
		} else if r.URL.Path == "/playlists/variants/low.m3u8" {
			playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
../segments/seg001.ts
#EXT-X-ENDLIST
`
			w.Write([]byte(playlist))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	info, err := ParsePlaylist(server.URL + "/playlists/master.m3u8")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify variant URL was resolved correctly
	expectedVariantURL := server.URL + "/playlists/variants/low.m3u8"
	if info.Variants[0].PlaylistURL != expectedVariantURL {
		t.Errorf("Expected variant URL %s, got %s", expectedVariantURL, info.Variants[0].PlaylistURL)
	}

	// Verify segment URL was resolved correctly relative to variant playlist
	expectedSegmentURL := server.URL + "/playlists/segments/seg001.ts"
	if info.Variants[0].Segments[0].URL != expectedSegmentURL {
		t.Errorf("Expected segment URL %s, got %s", expectedSegmentURL, info.Variants[0].Segments[0].URL)
	}
}

func TestParsePlaylist_InvalidM3U8(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not a valid m3u8 file"))
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error for invalid m3u8, got nil")
	}
}

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		relativeURL string
		expected    string
		shouldError bool
	}{
		{
			name:        "relative path",
			baseURL:     "http://example.com/path/playlist.m3u8",
			relativeURL: "segment.ts",
			expected:    "http://example.com/path/segment.ts",
			shouldError: false,
		},
		{
			name:        "absolute URL",
			baseURL:     "http://example.com/playlist.m3u8",
			relativeURL: "https://cdn.example.com/segment.ts",
			expected:    "https://cdn.example.com/segment.ts",
			shouldError: false,
		},
		{
			name:        "relative path with subdirectory",
			baseURL:     "http://example.com/playlist.m3u8",
			relativeURL: "segments/segment.ts",
			expected:    "http://example.com/segments/segment.ts",
			shouldError: false,
		},
		{
			name:        "root relative path",
			baseURL:     "http://example.com/path/playlist.m3u8",
			relativeURL: "/segments/segment.ts",
			expected:    "http://example.com/segments/segment.ts",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveURL(tt.baseURL, tt.relativeURL)
			if tt.shouldError && err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !tt.shouldError && err != nil {
				t.Fatalf("Expected no error, got %v", err)
			}
			if !tt.shouldError && result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
