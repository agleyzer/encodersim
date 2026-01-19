// The encodersim command converts static HLS playlists into continuously looping live HLS feeds.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agleyzer/encodersim/internal/parser"
	"github.com/agleyzer/encodersim/internal/playlist"
	"github.com/agleyzer/encodersim/internal/segment"
	"github.com/agleyzer/encodersim/internal/server"
	"github.com/agleyzer/encodersim/internal/variant"
)

const (
	version = "1.0.0"
)

func main() {
	// Parse command-line flags
	var (
		port        = flag.Int("port", 8080, "HTTP server port")
		windowSize  = flag.Int("window-size", 6, "Number of segments in sliding window")
		verbose     = flag.Bool("verbose", false, "Enable verbose logging")
		showVersion = flag.Bool("version", false, "Show version and exit")
		master      = flag.Bool("master", false, "Expect master playlist with multiple variants (auto-detected if not set)")
		variants    = flag.String("variants", "", "Comma-separated list of variant indices to serve (e.g., '0,2,4'). Serves all if not specified")
		loopAfter   = flag.String("loop-after", "", "Maximum duration of content to use before looping (e.g., '10s', '1m30s'). Uses all segments if not specified")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "EncoderSim - HLS Live Looping Tool v%s\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <playlist-url>\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Arguments:\n")
		fmt.Fprintf(os.Stderr, "  <playlist-url>    URL of the static HLS playlist (media or master)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://example.com/playlist.m3u8\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --port 8080 --window-size 6 https://example.com/playlist.m3u8\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --loop-after 10s https://example.com/playlist.m3u8\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --master https://example.com/master.m3u8\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s --master --variants 0,2 https://example.com/master.m3u8\n", os.Args[0])
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
	if err := run(playlistURL, *port, *windowSize, *master, *variants, *loopAfter, logger); err != nil {
		logger.Error("application error", "error", err)
		os.Exit(1)
	}

	logger.Info("EncoderSim stopped")
}

func run(playlistURL string, port, windowSize int, master bool, variants, loopAfter string, logger *slog.Logger) error {
	// Note: variants parameter for filtering variants will be implemented in future enhancement
	_ = variants

	// Parse and validate loop-after duration if specified
	var loopAfterDuration time.Duration
	if loopAfter != "" {
		duration, err := time.ParseDuration(loopAfter)
		if err != nil {
			return fmt.Errorf("invalid --loop-after duration '%s': %w", loopAfter, err)
		}
		if duration <= 0 {
			return fmt.Errorf("--loop-after duration must be positive, got: %s", loopAfter)
		}
		loopAfterDuration = duration
		logger.Info("loop-after specified", "duration", duration)
	}

	// Parse the source playlist
	logger.Info("fetching source playlist", "url", playlistURL)
	playlistInfo, err := parser.ParsePlaylist(playlistURL)
	if err != nil {
		return fmt.Errorf("failed to parse playlist: %w", err)
	}

	// Check if explicit mode is set, otherwise use detected mode
	if master && !playlistInfo.IsMaster {
		return fmt.Errorf("--master flag set but URL is a media playlist, not a master playlist")
	}

	// Create the live playlist generator based on playlist type
	var livePlaylist playlist.Playlist
	if playlistInfo.IsMaster {
		logger.Info("parsed master playlist",
			"variants", len(playlistInfo.Variants),
			"targetDuration", playlistInfo.TargetDuration,
		)

		// Apply loop-after to each variant if specified
		variants := playlistInfo.Variants
		if loopAfterDuration > 0 {
			// Create a copy of variants with subset segments
			variantsWithSubset := make([]variant.Variant, len(variants))
			for i, v := range variants {
				variantsWithSubset[i] = v
				variantsWithSubset[i].Segments = calculateSegmentSubset(v.Segments, loopAfterDuration)
				logger.Info("applied loop-after to variant",
					"variantIndex", i,
					"originalSegments", len(v.Segments),
					"includedSegments", len(variantsWithSubset[i].Segments),
					"duration", loopAfterDuration,
				)
			}
			variants = variantsWithSubset
		}

		// Log variant details
		for i, v := range variants {
			logger.Info("variant",
				"index", i,
				"bandwidth", v.Bandwidth,
				"resolution", v.Resolution,
				"segments", len(v.Segments),
			)
		}

		livePlaylist, err = playlist.NewMaster(
			variants,
			windowSize,
			logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create live master playlist: %w", err)
		}
	} else {
		logger.Info("parsed media playlist",
			"segments", len(playlistInfo.Segments),
			"targetDuration", playlistInfo.TargetDuration,
		)

		// Apply loop-after if specified
		segments := playlistInfo.Segments
		if loopAfterDuration > 0 {
			segments = calculateSegmentSubset(playlistInfo.Segments, loopAfterDuration)
			logger.Info("applied loop-after to media playlist",
				"originalSegments", len(playlistInfo.Segments),
				"includedSegments", len(segments),
				"duration", loopAfterDuration,
			)
		}

		livePlaylist, err = playlist.New(
			segments,
			windowSize,
			playlistInfo.TargetDuration,
			logger,
		)
		if err != nil {
			return fmt.Errorf("failed to create live playlist: %w", err)
		}
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

	if playlistInfo.IsMaster {
		logger.Info("live HLS stream ready (master playlist mode)",
			"master_url", fmt.Sprintf("http://localhost:%d/playlist.m3u8", port),
			"health", fmt.Sprintf("http://localhost:%d/health", port),
			"variants", len(playlistInfo.Variants),
		)
	} else {
		logger.Info("live HLS stream ready",
			"url", fmt.Sprintf("http://localhost:%d/playlist.m3u8", port),
			"health", fmt.Sprintf("http://localhost:%d/health", port),
		)
	}

	// Start server (blocks until shutdown)
	return srv.Start(ctx)
}

// calculateSegmentSubset returns a subset of segments that fit within the specified duration.
// It sums segment durations from the start until the threshold is reached.
// A segment is included if adding it doesn't exceed the threshold by more than 50%.
// Returns at least 1 segment even if the first segment exceeds the duration.
func calculateSegmentSubset(segments []segment.Segment, maxDuration time.Duration) []segment.Segment {
	if len(segments) == 0 {
		return segments
	}

	// If maxDuration is 0, return all segments
	if maxDuration == 0 {
		return segments
	}

	maxDurationSeconds := maxDuration.Seconds()
	var totalDuration float64
	var result []segment.Segment

	for i, seg := range segments {
		// Always include at least the first segment
		if i == 0 {
			result = append(result, seg)
			totalDuration += seg.Duration
			continue
		}

		// Check if adding this segment would exceed the threshold
		newTotal := totalDuration + seg.Duration
		if newTotal <= maxDurationSeconds {
			// Within threshold, include it
			result = append(result, seg)
			totalDuration = newTotal
		} else {
			// Would exceed threshold - check if we should include it anyway
			// Include if it doesn't exceed by more than 50%
			exceedAmount := newTotal - maxDurationSeconds
			if exceedAmount <= (maxDurationSeconds * 0.5) {
				result = append(result, seg)
				totalDuration = newTotal
			}
			// Stop processing further segments
			break
		}
	}

	return result
}
