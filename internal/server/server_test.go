package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agleyzer/encodersim/internal/playlist"
	"github.com/agleyzer/encodersim/pkg/segment"
)

func createTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func createTestPlaylist(t *testing.T) *playlist.LivePlaylist {
	segments := []segment.Segment{
		{URL: "https://example.com/seg1.ts", Duration: 10.0, Sequence: 0},
		{URL: "https://example.com/seg2.ts", Duration: 10.0, Sequence: 1},
		{URL: "https://example.com/seg3.ts", Duration: 10.0, Sequence: 2},
		{URL: "https://example.com/seg4.ts", Duration: 10.0, Sequence: 3},
		{URL: "https://example.com/seg5.ts", Duration: 10.0, Sequence: 4},
	}

	logger := createTestLogger()
	lp, err := playlist.New(segments, 3, 10, logger)
	if err != nil {
		t.Fatalf("Failed to create test playlist: %v", err)
	}
	return lp
}

func TestNew(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()

	srv := New(lp, 8080, logger)

	if srv.playlist != lp {
		t.Error("Playlist not set correctly")
	}
	if srv.port != 8080 {
		t.Error("Port not set correctly")
	}
	if srv.logger != logger {
		t.Error("Logger not set correctly")
	}
}

func TestHandlePlaylist(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	req := httptest.NewRequest("GET", "/playlist.m3u8", nil)
	w := httptest.NewRecorder()

	srv.handlePlaylist(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/vnd.apple.mpegurl" {
		t.Errorf("Expected Content-Type 'application/vnd.apple.mpegurl', got '%s'", contentType)
	}

	// Check cache control
	cacheControl := resp.Header.Get("Cache-Control")
	if !strings.Contains(cacheControl, "no-cache") {
		t.Errorf("Expected Cache-Control with 'no-cache', got '%s'", cacheControl)
	}

	// Check CORS header
	corsHeader := resp.Header.Get("Access-Control-Allow-Origin")
	if corsHeader != "*" {
		t.Errorf("Expected CORS header '*', got '%s'", corsHeader)
	}

	// Check body contains HLS content
	body := w.Body.String()
	if !strings.Contains(body, "#EXTM3U") {
		t.Error("Response body missing #EXTM3U tag")
	}
	if !strings.Contains(body, "#EXT-X-VERSION") {
		t.Error("Response body missing #EXT-X-VERSION tag")
	}
	if !strings.Contains(body, "#EXT-X-TARGETDURATION") {
		t.Error("Response body missing #EXT-X-TARGETDURATION tag")
	}
}

func TestHandleHealth(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Parse JSON response
	var health map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&health); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	// Check status field
	if health["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%v'", health["status"])
	}

	// Check stats field exists
	if _, ok := health["stats"]; !ok {
		t.Error("Health response missing 'stats' field")
	}

	// Check stats contains expected fields
	stats, ok := health["stats"].(map[string]interface{})
	if !ok {
		t.Fatal("Stats is not a map")
	}

	expectedFields := []string{"total_segments", "window_size", "current_position", "sequence_number", "target_duration"}
	for _, field := range expectedFields {
		if _, ok := stats[field]; !ok {
			t.Errorf("Stats missing field '%s'", field)
		}
	}
}

func TestHandleHealth_WithAdvancedPlaylist(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	// Advance the playlist a few times
	lp.Advance()
	lp.Advance()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	var health map[string]interface{}
	json.NewDecoder(w.Body).Decode(&health)

	stats := health["stats"].(map[string]interface{})

	// Check that position reflects advances
	position := stats["current_position"].(float64)
	if position != 2 {
		t.Errorf("Expected current_position 2, got %v", position)
	}

	sequence := stats["sequence_number"].(float64)
	if sequence != 2 {
		t.Errorf("Expected sequence_number 2, got %v", sequence)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	// Create a test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	wrapped := srv.loggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	// Check that handler was called
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "test" {
		t.Errorf("Expected body 'test', got '%s'", w.Body.String())
	}
}

func TestResponseWriter_CapturesStatusCode(t *testing.T) {
	wrapped := &responseWriter{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}

	wrapped.WriteHeader(http.StatusNotFound)

	if wrapped.statusCode != http.StatusNotFound {
		t.Errorf("Expected status code 404, got %d", wrapped.statusCode)
	}
}

func TestServer_Integration(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 0, logger) // Use port 0 for automatic port assignment

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Server should be running, cancel context to stop it
	cancel()

	// Wait for server to stop
	select {
	case err := <-errChan:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Expected nil or ErrServerClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Server did not stop within timeout")
	}
}

func TestHandlePlaylist_MultipleRequests(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	// Make multiple concurrent requests
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/playlist.m3u8", nil)
		w := httptest.NewRecorder()

		srv.handlePlaylist(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "#EXTM3U") {
			t.Errorf("Request %d: Response missing #EXTM3U", i)
		}
	}
}

func TestHandlePlaylist_WhileAdvancing(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-advance
	go lp.StartAutoAdvance(ctx)

	// Make requests while playlist is advancing
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/playlist.m3u8", nil)
		w := httptest.NewRecorder()

		srv.handlePlaylist(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		time.Sleep(200 * time.Millisecond)
	}

	cancel()
}

func TestHandleHealth_ConcurrentRequests(t *testing.T) {
	lp := createTestPlaylist(t)
	logger := createTestLogger()
	srv := New(lp, 8080, logger)

	done := make(chan bool)

	// Make concurrent health check requests
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()

			srv.handleHealth(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
