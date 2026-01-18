// Package server implements the HTTP server for serving live HLS playlists.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/agleyzer/encodersim/internal/playlist"
)

// Server serves the live HLS playlist.
type Server struct {
	playlist   *playlist.LivePlaylist
	port       int
	logger     *slog.Logger
	httpServer *http.Server
}

// New creates a new HTTP server.
func New(lp *playlist.LivePlaylist, port int, logger *slog.Logger) *Server {
	return &Server{
		playlist: lp,
		port:     port,
		logger:   logger,
	}
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Register handlers
	mux.HandleFunc("/playlist.m3u8", s.handlePlaylist)
	mux.HandleFunc("/health", s.handleHealth)

	// Register variant-specific handler (for master playlists)
	// This catches requests like /variant0/playlist.m3u8, /variant1/playlist.m3u8, etc.
	mux.HandleFunc("/", s.handleVariantPlaylist)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: s.loggingMiddleware(mux),
	}

	// Start server in a goroutine
	go func() {
		s.logger.Info("starting HTTP server", "port", s.port)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	s.logger.Info("shutting down HTTP server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(shutdownCtx)
}

// handlePlaylist serves the current live playlist.
// For media playlists, generates media playlist content.
// For master playlists, generates master playlist content.
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	// Check playlist mode and generate appropriate content
	stats := s.playlist.GetStats()
	isMaster, _ := stats["is_master"].(bool)

	var playlistContent string
	if isMaster {
		playlistContent = s.playlist.GenerateMaster()
	} else {
		playlistContent = s.playlist.Generate()
	}

	// Set HLS-specific headers
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Write the playlist
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(playlistContent))
}

// handleVariantPlaylist serves variant-specific media playlists.
// Handles requests like /variant0/playlist.m3u8, /variant1/playlist.m3u8, etc.
func (s *Server) handleVariantPlaylist(w http.ResponseWriter, r *http.Request) {
	// Only handle variant paths
	if !strings.HasPrefix(r.URL.Path, "/variant") || !strings.HasSuffix(r.URL.Path, "/playlist.m3u8") {
		// Not a variant playlist request, return 404
		http.NotFound(w, r)
		return
	}

	// Parse variant index from path
	// Path format: /variant{N}/playlist.m3u8
	path := strings.TrimPrefix(r.URL.Path, "/variant")
	path = strings.TrimSuffix(path, "/playlist.m3u8")

	variantIndex, err := strconv.Atoi(path)
	if err != nil {
		http.Error(w, "Invalid variant index", http.StatusBadRequest)
		return
	}

	// Generate variant-specific playlist
	playlistContent, err := s.playlist.GenerateVariant(variantIndex)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate variant playlist: %v", err), http.StatusNotFound)
		return
	}

	// Set HLS-specific headers
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Write the playlist
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(playlistContent))
}

// handleHealth serves health check information.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := s.playlist.GetStats()

	health := map[string]interface{}{
		"status": "ok",
		"stats":  stats,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(health)
}

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		s.logger.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
			"status", wrapped.statusCode,
			"duration", duration,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
