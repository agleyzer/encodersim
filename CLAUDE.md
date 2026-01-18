# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

EncoderSim is a Go CLI tool that converts static HLS playlists into continuously looping live HLS feeds. It's designed as a standalone executable, NOT a library - all packages are under `internal/` and cannot be imported by external projects.

## Essential Commands

### Building and Running
```bash
# Build the binary
go build -o encodersim ./cmd/encodersim

# Run the tool
./encodersim https://example.com/playlist.m3u8
./encodersim --port 8080 --window-size 6 --verbose https://example.com/playlist.m3u8

# Show version
./encodersim --version
```

### Testing
```bash
# Run all tests (unit + integration)
go test ./...

# Run only unit tests (skip integration)
go test -short ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...

# Run specific package tests
go test ./internal/parser
go test ./internal/playlist
go test ./internal/server

# Run integration tests (requires binary)
go build -o encodersim ./cmd/encodersim
go test -v ./test/integration

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Manual integration test with local HLS playlist
cd test && ./test.sh
```

### Code Quality
```bash
# Format code (REQUIRED before commits)
gofmt -w .

# Vet code for common issues
go vet ./...

# Check imports
go mod tidy
go mod verify
```

## Architecture

### Component Overview

1. **cmd/encodersim/main.go**: CLI entry point
   - Parses command-line flags (port, window-size, verbose, version)
   - Validates inputs (port 1-65535, window-size >= 1)
   - Orchestrates component initialization
   - Manages graceful shutdown via signal handling

2. **internal/parser**: HLS playlist fetching and parsing
   - `ParsePlaylist()`: Fetches m3u8 from URL, returns PlaylistInfo
   - Uses `github.com/grafov/m3u8` library
   - Auto-detects master vs media playlists
   - For master playlists: parses variants, fetches each variant's media playlist
   - For media playlists: parses segments directly
   - Resolves relative URLs (variant playlists and segments) to absolute URLs
   - Calculates target duration if not specified in playlist

3. **internal/playlist**: Live playlist generation with sliding window
   - `LivePlaylist`: Thread-safe with sync.RWMutex
   - Supports both media playlist mode and master playlist mode
   - `Generate()`: Creates HLS m3u8 media playlist for current window
   - `GenerateMaster()`: Creates HLS master playlist with variant links
   - `GenerateVariant(index)`: Creates media playlist for specific variant
   - `Advance()`: Moves window forward (all variants synchronously in master mode)
   - `StartAutoAdvance()`: Goroutine that advances window based on target duration
   - `GetStats()`: Returns current state (per-variant in master mode)
   - **Discontinuity detection**: Automatically inserts `#EXT-X-DISCONTINUITY` tag when playlist loops back to start (per-variant in master mode)

4. **internal/server**: HTTP server
   - `GET /playlist.m3u8`: Serves current live playlist (master or media)
   - `GET /variant0/playlist.m3u8`, `/variant1/playlist.m3u8`, etc.: Variant playlists (master mode only)
   - `GET /health`: Returns JSON with statistics (per-variant in master mode)
   - Logging middleware for all requests
   - Graceful shutdown with 10-second timeout

5. **internal/segment**: Shared data structures
   - `Segment` struct: URL, Duration, Sequence, VariantIndex

6. **internal/variant**: Multi-variant data structures
   - `Variant` struct: Bandwidth, Resolution, Codecs, PlaylistURL, Segments, TargetDuration

7. **test/integration**: Integration test framework
   - `TestHarness`: Manages test environment (HTTP server + encodersim binary)
   - Automatically starts HTTP server serving test playlists
   - Launches encodersim binary as subprocess
   - Provides playlist parsing and verification helpers
   - `WaitForCondition()`: Polls until expected conditions are met
   - Tests verify end-to-end behavior including wrapping and discontinuity tags
   - Master playlist tests verify multi-variant synchronization

### Key Design Patterns

- **Sliding Window**: Maintains a configurable window (default: 6 segments) that advances every target duration
- **Infinite Looping**: When window reaches end of segments, wraps around to beginning (modulo arithmetic)
- **Discontinuity Signaling**: Detects loop points by comparing segment sequence numbers, inserts `#EXT-X-DISCONTINUITY` tag per HLS spec
- **Thread Safety**: RWMutex protects shared state in LivePlaylist (multiple readers, single writer)
- **Graceful Shutdown**: Context-based cancellation propagates through goroutines
- **Multi-Variant Synchronization**: All variants advance together on a single global timer based on maximum target duration across variants

### Data Flow
1. User provides static HLS playlist URL
2. Parser fetches and parses m3u8, resolves segment URLs
3. LivePlaylist initialized with segments, window size, target duration
4. HTTP server starts, auto-advance goroutine begins
5. Clients request `/playlist.m3u8`, server generates current window
6. Window advances automatically every `target_duration` seconds
7. Loop continues infinitely until shutdown signal

## Critical Implementation Rules

### Google Go Style Guide (MANDATORY)
This project strictly follows the [Google Go Style Guide](https://google.github.io/styleguide/go). Key requirements:

- **Package comments**: Required above `package` declaration, no blank line between comment and package
- **Function documentation**: All exported functions need doc comments starting with function name
- **Error handling**: Use `fmt.Errorf` with `%w` for wrapping. Error strings NOT capitalized, NO punctuation
- **Import grouping**: (1) stdlib, (2) project packages, (3) third-party - separated by blank lines
- **Code formatting**: ALL code must pass `gofmt` without changes
- **Context usage**: `context.Context` always first parameter (except HTTP handlers)
- **Naming**: Use `MixedCaps`, not underscores. Receivers: short and consistent

### Project-Specific Rules

1. **This is a CLI tool, not a library**
   - All packages MUST be under `internal/` (enforced by Go compiler)
   - Do NOT create `pkg/` directory
   - No exported APIs for external consumption

2. **No segment downloading**
   - Tool only manipulates m3u8 manifests
   - Clients fetch segments directly from original URLs
   - Never cache or proxy video segments

3. **Thread safety**
   - Use sync.RWMutex for LivePlaylist state
   - Multiple readers (playlist generation) OK
   - Single writer (window advancement)
   - Run `go test -race` to verify

4. **Test coverage targets**
   - Overall: >= 60%
   - internal/playlist: >= 95% (critical business logic)
   - internal/server: >= 90%
   - internal/parser: >= 60%

5. **Dependencies**
   - ONLY external dependency: `github.com/grafov/m3u8`
   - Use Go stdlib for everything else
   - No GPL-licensed dependencies (MIT/BSD/Apache 2.0 only)

## Common Development Tasks

### Adding a new feature
1. Read SPEC.md for requirements and constraints
2. Implement in appropriate internal package
3. Follow Google Go style guide
4. Write table-driven tests
5. Run `gofmt -w .` before committing
6. Verify with `go test -race ./...`

### Debugging
```bash
# Run with verbose logging
./encodersim --verbose https://example.com/playlist.m3u8

# Test locally with test.sh
cd test && ./test.sh

# Check current playlist state
curl http://localhost:8080/playlist.m3u8

# Check health/stats
curl http://localhost:8080/health
```

### Modifying sliding window behavior
- Core logic: `internal/playlist/generator.go`
- Window calculation: `getCurrentWindow()` method uses modulo arithmetic
- Discontinuity detection: `Generate()` method compares segment sequences
- Advancement timing: `StartAutoAdvance()` uses target duration

### Understanding HLS compliance
- Version 3 required tags: `#EXTM3U`, `#EXT-X-VERSION:3`, `#EXT-X-TARGETDURATION`, `#EXT-X-MEDIA-SEQUENCE`
- Per-segment tag: `#EXTINF:<duration>,`
- Live stream: NEVER include `#EXT-X-ENDLIST` tag
- Loop signaling: `#EXT-X-DISCONTINUITY` before first segment after wrap-around

## References

- [SPEC.md](SPEC.md) - Complete technical specification
- [README.md](README.md) - User-facing documentation
- [HLS RFC 8216](https://datatracker.ietf.org/doc/html/rfc8216) - HLS specification
- [Google Go Style Guide](https://google.github.io/styleguide/go) - Required style guide
