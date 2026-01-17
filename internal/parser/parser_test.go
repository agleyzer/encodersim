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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playlist := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000
low.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000
high.m3u8
`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(playlist))
	}))
	defer server.Close()

	_, err := ParsePlaylist(server.URL)
	if err == nil {
		t.Fatal("Expected error for master playlist, got nil")
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
