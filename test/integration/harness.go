// Package integration provides integration testing utilities for EncoderSim.
package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHarness manages the test environment for integration tests.
type TestHarness struct {
	t              *testing.T
	httpServer     *http.Server
	httpPort       int
	encodersimCmd  *exec.Cmd
	encodersimPort int
	testDataDir    string
	tempDir        string // Track temp directory for adding playlists
	cancel         context.CancelFunc
}

// NewTestHarness creates a new test harness.
func NewTestHarness(t *testing.T) *TestHarness {
	t.Helper()

	// Find available ports
	httpPort := findAvailablePort(t)
	encodersimPort := findAvailablePort(t)

	// Determine test data directory
	testDataDir := filepath.Join(".", "testdata")
	if _, err := os.Stat(testDataDir); os.IsNotExist(err) {
		// We might be running from test/integration directory
		testDataDir = "./testdata"
	}

	return &TestHarness{
		t:              t,
		httpPort:       httpPort,
		encodersimPort: encodersimPort,
		testDataDir:    testDataDir,
	}
}

// StartHTTPServer starts an HTTP server serving test playlists.
func (h *TestHarness) StartHTTPServer(playlistContent string, playlistName string) {
	h.t.Helper()

	// Create a temporary directory for test files
	h.tempDir = h.t.TempDir()

	// Write playlist content to file
	playlistPath := filepath.Join(h.tempDir, playlistName)
	if err := os.WriteFile(playlistPath, []byte(playlistContent), 0644); err != nil {
		h.t.Fatalf("failed to write test playlist: %v", err)
	}

	// Create file server
	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(h.tempDir))
	mux.Handle("/", fileServer)

	h.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.httpPort),
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		if err := h.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.t.Logf("HTTP server error: %v", err)
		}
	}()

	// Wait for server to be ready
	h.waitForServer(fmt.Sprintf("http://localhost:%d", h.httpPort), 5*time.Second)
	h.t.Logf("HTTP server started on port %d", h.httpPort)
}

// AddPlaylist adds an additional playlist to the HTTP server.
// Must be called after StartHTTPServer.
func (h *TestHarness) AddPlaylist(playlistContent string, playlistName string) {
	h.t.Helper()

	if h.tempDir == "" {
		h.t.Fatal("StartHTTPServer must be called before AddPlaylist")
	}

	// Write playlist content to file in the same temp directory
	playlistPath := filepath.Join(h.tempDir, playlistName)
	if err := os.WriteFile(playlistPath, []byte(playlistContent), 0644); err != nil {
		h.t.Fatalf("failed to write additional playlist: %v", err)
	}

	h.t.Logf("Added playlist: %s", playlistName)
}

// StartEncoderSim starts the encodersim binary pointing to the test playlist.
func (h *TestHarness) StartEncoderSim(playlistName string, windowSize int) {
	h.t.Helper()

	// Find encodersim binary
	binaryPath := h.findEncoderSimBinary()

	// Build the command
	playlistURL := fmt.Sprintf("http://localhost:%d/%s", h.httpPort, playlistName)
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel

	h.encodersimCmd = exec.CommandContext(ctx, binaryPath,
		"--port", fmt.Sprintf("%d", h.encodersimPort),
		"--window-size", fmt.Sprintf("%d", windowSize),
		playlistURL,
	)

	// Capture output for debugging
	h.encodersimCmd.Stdout = os.Stdout
	h.encodersimCmd.Stderr = os.Stderr

	// Start encodersim
	if err := h.encodersimCmd.Start(); err != nil {
		h.t.Fatalf("failed to start encodersim: %v", err)
	}

	// Wait for encodersim to be ready
	encodersimURL := fmt.Sprintf("http://localhost:%d/health", h.encodersimPort)
	h.waitForServer(encodersimURL, 10*time.Second)
	h.t.Logf("EncoderSim started on port %d", h.encodersimPort)
}

// FetchPlaylist fetches the current playlist from encodersim.
func (h *TestHarness) FetchPlaylist() string {
	h.t.Helper()

	url := fmt.Sprintf("http://localhost:%d/playlist.m3u8", h.encodersimPort)
	resp, err := http.Get(url)
	if err != nil {
		h.t.Fatalf("failed to fetch playlist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("failed to read playlist body: %v", err)
	}

	return string(body)
}

// FetchVariantPlaylist fetches a variant playlist from encodersim.
func (h *TestHarness) FetchVariantPlaylist(variantIndex int) string {
	h.t.Helper()

	url := fmt.Sprintf("http://localhost:%d/variant%d/playlist.m3u8", h.encodersimPort, variantIndex)
	resp, err := http.Get(url)
	if err != nil {
		h.t.Fatalf("failed to fetch variant %d playlist: %v", variantIndex, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("unexpected status code for variant %d: %d", variantIndex, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("failed to read variant %d playlist body: %v", variantIndex, err)
	}

	return string(body)
}

// FetchHealth fetches the health endpoint and returns the JSON response.
func (h *TestHarness) FetchHealth() string {
	h.t.Helper()

	url := fmt.Sprintf("http://localhost:%d/health", h.encodersimPort)
	resp, err := http.Get(url)
	if err != nil {
		h.t.Fatalf("failed to fetch health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("failed to read health body: %v", err)
	}

	return string(body)
}

// Cleanup stops all running services.
func (h *TestHarness) Cleanup() {
	h.t.Helper()

	// Stop encodersim
	if h.cancel != nil {
		h.cancel()
	}
	if h.encodersimCmd != nil && h.encodersimCmd.Process != nil {
		h.encodersimCmd.Process.Kill()
		h.encodersimCmd.Wait()
	}

	// Stop HTTP server
	if h.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.httpServer.Shutdown(ctx)
	}
}

// findEncoderSimBinary locates the encodersim binary.
func (h *TestHarness) findEncoderSimBinary() string {
	h.t.Helper()

	// Try several possible locations
	candidates := []string{
		"../../encodersim",            // From test/integration
		"./encodersim",                // From project root
		"../encodersim",               // From test directory
		"./cmd/encodersim/encodersim", // Built in place
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			absPath, _ := filepath.Abs(path)
			h.t.Logf("Found encodersim binary at: %s", absPath)
			return absPath
		}
	}

	h.t.Fatal("encodersim binary not found. Run 'go build -o encodersim ./cmd/encodersim' first")
	return ""
}

// waitForServer waits for a server to become available.
func (h *TestHarness) waitForServer(url string, timeout time.Duration) {
	h.t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	h.t.Fatalf("server at %s did not become available within %v", url, timeout)
}

// findAvailablePort finds an available TCP port.
func findAvailablePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

// ParsedPlaylist represents a parsed HLS playlist for testing.
type ParsedPlaylist struct {
	Version        int
	TargetDuration int
	MediaSequence  uint64
	Segments       []PlaylistSegment
	HasEndList     bool
}

// PlaylistSegment represents a segment in a playlist.
type PlaylistSegment struct {
	Duration      float64
	URL           string
	Discontinuity bool
}

// ParsePlaylist parses an HLS playlist into a structured format.
func ParsePlaylist(content string) *ParsedPlaylist {
	playlist := &ParsedPlaylist{
		Segments: []PlaylistSegment{},
	}

	lines := strings.Split(content, "\n")
	var currentSegment *PlaylistSegment
	var nextSegmentHasDiscontinuity bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			fmt.Sscanf(line, "#EXT-X-VERSION:%d", &playlist.Version)

		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			fmt.Sscanf(line, "#EXT-X-TARGETDURATION:%d", &playlist.TargetDuration)

		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			fmt.Sscanf(line, "#EXT-X-MEDIA-SEQUENCE:%d", &playlist.MediaSequence)

		case line == "#EXT-X-ENDLIST":
			playlist.HasEndList = true

		case line == "#EXT-X-DISCONTINUITY":
			// Mark that the next segment should have discontinuity flag
			nextSegmentHasDiscontinuity = true

		case strings.HasPrefix(line, "#EXTINF:"):
			currentSegment = &PlaylistSegment{}
			fmt.Sscanf(line, "#EXTINF:%f,", &currentSegment.Duration)
			// Apply discontinuity flag if it was set
			if nextSegmentHasDiscontinuity {
				currentSegment.Discontinuity = true
				nextSegmentHasDiscontinuity = false
			}

		case !strings.HasPrefix(line, "#"):
			// This is a segment URL
			if currentSegment != nil {
				currentSegment.URL = line
				playlist.Segments = append(playlist.Segments, *currentSegment)
				currentSegment = nil
			}
		}
	}

	return playlist
}

// WaitForCondition polls until a condition is met or timeout occurs.
func (h *TestHarness) WaitForCondition(condition func() bool, timeout time.Duration, description string) {
	h.t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				h.t.Fatalf("timeout waiting for condition: %s", description)
			}
		}
	}
}
