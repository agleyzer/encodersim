package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/agleyzer/encodersim/internal/parser"
	"github.com/agleyzer/encodersim/internal/playlist"
	"github.com/agleyzer/encodersim/internal/server"
)

const (
	version = "1.0.0"
)

func main() {
	// Parse command-line flags
	var (
		port       = flag.Int("port", 8080, "HTTP server port")
		windowSize = flag.Int("window-size", 6, "Number of segments in sliding window")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
		showVersion = flag.Bool("version", false, "Show version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "EncoderSim - HLS Live Looping Tool v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <playlist-url>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  <playlist-url>    URL of the static HLS playlist\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s https://example.com/playlist.m3u8\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --port 8080 --window-size 6 https://example.com/playlist.m3u8\n", os.Args[0])
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("EncoderSim v%s\n", version)
		os.Exit(0)
	}

	// Check for playlist URL argument
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: playlist URL is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	playlistURL := flag.Arg(0)

	// Validate flags
	if *port < 1 || *port > 65535 {
		fmt.Fprintf(os.Stderr, "Error: port must be between 1 and 65535\n")
		os.Exit(1)
	}

	if *windowSize < 1 {
		fmt.Fprintf(os.Stderr, "Error: window size must be at least 1\n")
		os.Exit(1)
	}

	// Setup logger
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	logger.Info("EncoderSim starting", "version", version)

	// Run the application
	if err := run(playlistURL, *port, *windowSize, logger); err != nil {
		logger.Error("application error", "error", err)
		os.Exit(1)
	}

	logger.Info("EncoderSim stopped")
}

func run(playlistURL string, port, windowSize int, logger *slog.Logger) error {
	// Parse the source playlist
	logger.Info("fetching source playlist", "url", playlistURL)
	playlistInfo, err := parser.ParsePlaylist(playlistURL)
	if err != nil {
		return fmt.Errorf("failed to parse playlist: %w", err)
	}

	logger.Info("parsed playlist",
		"segments", len(playlistInfo.Segments),
		"targetDuration", playlistInfo.TargetDuration,
	)

	// Create the live playlist generator
	livePlaylist, err := playlist.New(
		playlistInfo.Segments,
		windowSize,
		playlistInfo.TargetDuration,
		logger,
	)
	if err != nil {
		return fmt.Errorf("failed to create live playlist: %w", err)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal", "signal", sig)
		cancel()
	}()

	// Start auto-advance in a goroutine
	go livePlaylist.StartAutoAdvance(ctx)

	// Create and start the HTTP server
	srv := server.New(livePlaylist, port, logger)

	logger.Info("live HLS stream ready",
		"url", fmt.Sprintf("http://localhost:%d/playlist.m3u8", port),
		"health", fmt.Sprintf("http://localhost:%d/health", port),
	)

	// Start server (blocks until shutdown)
	return srv.Start(ctx)
}
