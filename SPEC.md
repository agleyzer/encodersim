# EncoderSim - Technical Specification

## Overview

EncoderSim is a Go command-line tool that converts static HLS (HTTP Live Streaming) playlists into continuously looping live HLS feeds. The tool manipulates only the HLS manifest files (m3u8) without downloading or caching video segments, presenting a sliding window of segments that loops infinitely.

**Project Type:** CLI Application (not a library)
**Version:** 1.0.0
**Language:** Go 1.21+
**License:** MIT

**Note:** EncoderSim is designed as a standalone executable binary. It is **not intended to be imported as a library** by other Go projects. All packages are private (under `internal/`) and cannot be imported externally, which is enforced by the Go compiler.

## Project Goals

1. Convert static VOD (Video on Demand) HLS playlists into live streaming feeds
2. Create realistic live streaming behavior using a sliding window approach
3. Loop content infinitely without user intervention
4. Maintain HLS specification compliance
5. Provide a simple, easy-to-use CLI interface

## Core Requirements

### 1. Input Requirements

**MUST:**
- Accept a URL to a static HLS playlist (m3u8) as input
  - Support media playlists (single variant)
  - Support master playlists (multi-variant/multi-bitrate)
- Support both HTTP and HTTPS URLs
- Parse HLS version 3+ playlists
- Handle both relative and absolute segment URLs
- Resolve relative segment URLs to absolute URLs based on playlist location
- Auto-detect playlist type (master vs media)

**MUST NOT:**
- Download or cache video segments
- Modify original segment URLs (except for resolution to absolute paths)

### 2. Playlist Processing

**MUST:**
- Parse m3u8 files using a compliant HLS parser
- Extract segment information:
  - Segment URLs
  - Segment durations (`#EXTINF`)
  - Target duration (`#EXT-X-TARGETDURATION`)
- Handle playlists without explicit target duration by calculating from max segment duration
- Reject empty playlists (zero segments)

### 3. Live Playlist Generation

**MUST:**
- Generate HLS-compliant live playlists
- Implement a sliding window over the source segments
- Default window size: 6 segments (configurable)
- Include required HLS tags:
  - `#EXTM3U`
  - `#EXT-X-VERSION:3`
  - `#EXT-X-TARGETDURATION:<duration>`
  - `#EXT-X-MEDIA-SEQUENCE:<sequence>`
  - `#EXTINF:<duration>,` for each segment
- **NOT** include `#EXT-X-ENDLIST` tag (indicates live stream)
- Preserve original segment URLs in generated playlist

### 4. Sliding Window Behavior

**MUST:**
- Maintain a configurable window of N segments (default: 6)
- Advance the window by one segment at regular intervals
- Use target duration as the advancement interval
- Loop back to the beginning when reaching the end of the source playlist
- Increment `#EXT-X-MEDIA-SEQUENCE` on each window advancement
- Continue infinitely until stopped by user

### 5. Discontinuity Handling

**MUST:**
- Insert `#EXT-X-DISCONTINUITY` tag when the playlist loops
- Detect loop points by comparing segment sequence numbers
- Place discontinuity tag before the first segment after loop point
- Follow HLS specification for discontinuity signaling

**Example:**
```m3u8
#EXTINF:10.000,
https://example.com/segment008.ts
#EXT-X-DISCONTINUITY
#EXTINF:9.900,
https://example.com/segment001.ts
```

### 6. HTTP Server

**MUST:**
- Run an HTTP server to serve the live playlist
- Default port: 8080 (configurable)
- Implement endpoints:
  - `GET /playlist.m3u8` - Current live playlist
  - `GET /health` - Health check and statistics
- Set proper HTTP headers:
  - Content-Type: `application/vnd.apple.mpegurl` for m3u8
  - Cache-Control: `no-cache, no-store, must-revalidate`
  - Access-Control-Allow-Origin: `*` (CORS)
- Support concurrent client requests
- Thread-safe playlist generation

**MUST NOT:**
- Serve video segments (clients fetch from original URLs)
- Cache or proxy segment requests

### 7. Health Endpoint

**MUST:**
- Return JSON with status and statistics
- Include fields:
  - `status`: "ok"
  - `stats`: object with different fields based on playlist type

**Media Playlist Response:**
```json
{
  "status": "ok",
  "stats": {
    "is_master": false,
    "total_segments": 30,
    "window_size": 6,
    "current_position": 12,
    "sequence_number": 42,
    "target_duration": 10
  }
}
```

**Master Playlist Response:**
```json
{
  "status": "ok",
  "stats": {
    "is_master": true,
    "window_size": 6,
    "sequence_number": 42,
    "target_duration": 10,
    "variant_count": 2,
    "variants": [
      {
        "index": 0,
        "bandwidth": 1280000,
        "resolution": "640x360",
        "total_segments": 30,
        "position": 12
      },
      {
        "index": 1,
        "bandwidth": 2560000,
        "resolution": "1280x720",
        "total_segments": 30,
        "position": 12
      }
    ]
  }
}
```

### 8. Command-Line Interface

**MUST:**
- Accept playlist URL as positional argument
- Support flags:
  - `--port <int>`: HTTP server port (default: 8080, range: 1-65535)
  - `--window-size <int>`: Sliding window size (default: 6, minimum: 1)
  - `--loop-after <duration>`: Maximum content duration before looping (e.g., "10s", "1m30s")
  - `--master`: Explicitly expect master playlist (auto-detected if not set)
  - `--variants <string>`: Comma-separated variant indices to serve (future enhancement)
  - `--verbose`: Enable verbose/debug logging
  - `--version`: Show version and exit
- Display usage help with `--help` or when URL is missing
- Validate all inputs before execution
- Exit with appropriate error codes (0 for success, 1 for errors)

**Example Usage:**
```bash
encodersim https://example.com/playlist.m3u8
encodersim --port 8080 --window-size 6 https://example.com/playlist.m3u8
```

### 9. Error Handling

**MUST:**
- Handle network errors gracefully
- Provide clear error messages for:
  - Invalid URLs
  - HTTP errors (404, 500, etc.)
  - Invalid m3u8 format
  - Empty playlists
  - Master playlists with no variants
  - Invalid command-line arguments
- Use Go error wrapping (`fmt.Errorf` with `%w`)
- Error messages must not be capitalized (except proper nouns/acronyms)
- Error messages must not end with punctuation

### 10. Logging

**MUST:**
- Use structured logging (`log/slog`)
- Log levels: Debug, Info, Error
- Default level: Info
- Verbose mode: Debug level
- Log key events:
  - Application start/stop
  - Playlist fetching and parsing
  - HTTP server start
  - Window advancement (debug level)
  - HTTP requests (info level)
  - Errors (error level)

### 11. Graceful Shutdown

**MUST:**
- Handle SIGINT (Ctrl+C) and SIGTERM signals
- Stop window advancement goroutine
- Shutdown HTTP server gracefully
- Wait for in-flight requests to complete (10 second timeout)
- Log shutdown initiation and completion

### 12. Concurrency

**MUST:**
- Use goroutines for:
  - Window auto-advancement
  - HTTP server
  - Signal handling
- Protect shared state with `sync.RWMutex`
- Use `context.Context` for cancellation propagation
- Support concurrent HTTP requests without data races

## Code Quality Requirements

### ⭐ Google Go Style Guide Compliance (CRITICAL)

**MUST FOLLOW:** [Google Go Style Guide](https://google.github.io/styleguide/go)

**Specific Requirements:**

1. **Package Comments**
   - Every package must have a doc comment immediately above the `package` clause
   - No blank line between comment and package declaration
   - Format: "Package X provides/implements Y" for libraries
   - Format: "The X command does Y" for main packages
   - Complete sentences starting with package name

2. **Function Documentation**
   - All exported functions/methods must have doc comments
   - Doc comments must be complete sentences
   - Start with the function name
   - End with a period
   - Appear immediately above the declaration

3. **Naming Conventions**
   - Use `MixedCaps` or `mixedCaps` (never underscores)
   - Exported names start with capital letter
   - Unexported names start with lowercase
   - Interface names typically end in `-er` (e.g., `Reader`)
   - Receiver names should be consistent and short

4. **Code Formatting**
   - All code must pass `gofmt` without changes
   - No manual formatting adjustments

5. **Error Handling**
   - Return errors as final return value
   - Use `error` type (not custom error interfaces without good reason)
   - Wrap errors with context using `fmt.Errorf` with `%w`
   - Error strings not capitalized (unless starting with proper noun)
   - Error strings do not end with punctuation

6. **Context Usage**
   - `context.Context` always first parameter (except HTTP handlers)
   - Never create custom context types
   - Always check context cancellation in long-running operations

7. **Import Grouping**
   - Standard library packages (first group)
   - Project packages (second group)
   - Third-party packages (third group)
   - Blank imports for side effects (last)
   - Groups separated by blank lines

8. **Variable Declarations**
   - Use `var t []string` for nil slices (not `t := []string{}`)
   - Use `:=` for short variable declarations when appropriate
   - Declare variables close to first use

9. **Comments**
   - Explain *why*, not *what*
   - Avoid redundant comments
   - Complete sentences
   - Proper capitalization and punctuation

### Code Organization

**IMPORTANT:** EncoderSim is a **CLI tool**, not a library. It is designed to be executed as a standalone binary, not imported by other Go projects. Therefore, **all packages are private** and placed under `internal/`.

**MUST:**
- Follow standard Go project layout for CLI tools:
  ```
  encodersim/
  ├── cmd/encodersim/         # Main application entry point
  ├── internal/               # All private packages (CLI tools only)
  │   ├── parser/            # HLS parsing
  │   ├── playlist/          # Live playlist generation
  │   ├── segment/           # Segment data structures
  │   └── server/            # HTTP server
  └── test/                  # Test resources
  ```
- Use `internal/` for ALL packages (enforced by Go compiler)
  - Prevents external projects from importing our code
  - Appropriate for CLI applications that aren't meant to be libraries
- One package per directory

**MUST NOT:**
- Create `pkg/` directory - this is a CLI tool, not a library
- Export packages for external use - all code is implementation details
- Design APIs for external consumption - users interact via CLI only

### Testing Requirements

**MUST:**
- Achieve minimum 60% overall test coverage
- Test files named `*_test.go`
- Test functions named `Test*`
- Use table-driven tests where appropriate
- Include unit tests for:
  - Parser functionality
  - Playlist generation
  - Window advancement and looping
  - Discontinuity detection
  - HTTP handlers
  - Concurrent access patterns
- Include integration tests for:
  - End-to-end playlist serving
  - Server lifecycle
- Use `httptest` for HTTP testing
- Mock external dependencies (HTTP servers)

**Target Coverage:**
- `internal/parser`: ≥60%
- `internal/playlist`: ≥95% (critical business logic)
- `internal/server`: ≥90%
- Overall: ≥60%

### Dependencies

**MUST USE:**
- `github.com/grafov/m3u8` - HLS playlist parsing
- Go standard library for everything else:
  - `net/http` - HTTP client and server
  - `log/slog` - Structured logging
  - `context` - Cancellation
  - `flag` - CLI parsing

**MUST NOT:**
- Add unnecessary external dependencies
- Use deprecated packages
- Include GPL-licensed dependencies (MIT/BSD/Apache 2.0 only)

## Architecture

### Components

1. **Parser** (`internal/parser`)
   - Fetches m3u8 from URL
   - Parses using grafov/m3u8 library
   - Resolves relative URLs
   - Returns `PlaylistInfo` struct

2. **Playlist Generator** (`internal/playlist`)
   - Manages sliding window state
   - Generates live m3u8 content
   - Handles window advancement
   - Detects and marks discontinuities
   - Thread-safe with RWMutex

3. **HTTP Server** (`internal/server`)
   - Serves playlist endpoint
   - Serves health endpoint
   - Logs requests
   - Handles shutdown gracefully

4. **Main** (`cmd/encodersim`)
   - CLI argument parsing
   - Component initialization
   - Orchestration and lifecycle management

### Data Flow

```
1. User provides playlist URL
2. Parser fetches and parses m3u8
3. LivePlaylist initialized with segments
4. HTTP server starts
5. Auto-advance goroutine starts
6. Clients request /playlist.m3u8
7. Server generates current window
8. Window advances every N seconds
9. Loop repeats indefinitely
```

### Thread Safety

- Use `sync.RWMutex` for `LivePlaylist` state
- Multiple readers (playlist generation)
- Single writer (window advancement)
- No data races allowed

## Performance Requirements

**MUST:**
- Start up in less than 5 seconds for typical playlists (<100 segments)
- Handle at least 100 concurrent HTTP clients
- Memory usage scales linearly with segment count (metadata only)
- CPU usage minimal during steady state (only timer-based advancement)
- No memory leaks during long-running operation (24+ hours)

**Target Metrics:**
- Playlist generation: <10ms
- HTTP request latency: <50ms (p99)
- Memory footprint: <50MB for typical use case

## Security Requirements

**MUST:**
- Validate all user inputs (URLs, flags)
- Use timeouts for HTTP operations (30 seconds)
- Limit port range (1-65535)
- No arbitrary code execution
- No path traversal vulnerabilities
- Graceful handling of malicious m3u8 content

**MUST NOT:**
- Execute shell commands
- Write to filesystem (except standard output)
- Accept file:// URLs (HTTP/HTTPS only)
- Follow unlimited redirects

## Compatibility Requirements

**MUST:**
- Support Go 1.21 or higher (for `log/slog`)
- Run on Linux, macOS, and Windows
- Generate HLS version 3 compliant playlists
- Work with standard HLS players (VLC, ffplay, iOS Safari, Android)

## Documentation Requirements

**MUST:**
- README.md with:
  - Project description
  - Installation instructions
  - Usage examples
  - Configuration options
  - Architecture overview
- SPEC.md (this document)
- Code comments following Google style guide
- Examples in test/ directory

## Testing and Validation

### Manual Testing Checklist

- [ ] Parse valid static playlist
- [ ] Generate live playlist with correct tags
- [ ] Window advances at correct intervals
- [ ] Playlist loops correctly
- [ ] Discontinuity tag appears at loop point
- [ ] HTTP server responds to requests
- [ ] Health endpoint returns correct JSON
- [ ] Concurrent requests work correctly
- [ ] Graceful shutdown on Ctrl+C
- [ ] VLC can play the live stream
- [ ] ffplay can play the live stream

### Automated Testing

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Coverage targets met
- [ ] No data races detected (`go test -race`)
- [ ] No linter warnings (`go vet`)
- [ ] Code formatted with `gofmt`

## Success Criteria

The project is considered complete when:

1. ✅ All MUST requirements are implemented
2. ✅ Google Go style guide is followed throughout codebase
3. ✅ All tests pass with adequate coverage
4. ✅ Manual testing checklist completed
5. ✅ Documentation is complete
6. ✅ Binary builds without errors
7. ✅ Tool works with real-world HLS playlists
8. ✅ No known critical bugs

## Multi-Variant Playlist Support

EncoderSim supports HLS master playlists with multiple bitrate/quality variants.

**Master Playlist Mode:**
- Automatically detects master playlists during parsing
- Fetches and parses all variant media playlists
- Maintains separate sliding windows for each variant
- Each variant advances independently based on its own target duration
- Generates compliant master playlist with `#EXT-X-STREAM-INF` tags

**URL Structure:**
- Master playlist: `GET /playlist.m3u8`
- Variant playlists: `GET /variant/0/playlist.m3u8`, `/variant/1/playlist.m3u8`, etc.
- Health endpoint includes per-variant statistics

**Variant Management:**
- Each variant runs its own independent auto-advance goroutine
- Each variant advances based on its own target duration
- Variants with the same target duration naturally stay synchronized
- Each variant maintains independent discontinuity detection
- Sliding windows wrap independently when variants have different lengths

## Loop-After Feature

The `--loop-after` flag limits the amount of content used from the source playlist before looping.

**MUST:**
- Accept duration strings in Go time.Duration format (e.g., "10s", "1m30s", "2h")
- Validate that duration is positive (reject zero or negative values)
- Calculate segment subset based on cumulative segment durations
- Always include at least the first segment, even if it exceeds duration
- Apply 50% threshold rule: include boundary segment if it doesn't exceed duration by more than 50%
- Apply to both media playlists and all variants in master playlists independently
- Log original vs included segment counts when loop-after is applied

**Algorithm:**
1. If maxDuration is 0 or not specified, include all segments
2. Always include first segment regardless of its duration
3. For subsequent segments:
   - Calculate newTotal = currentTotal + segment.Duration
   - If newTotal <= maxDuration, include segment
   - If newTotal > maxDuration:
     - Calculate exceedAmount = newTotal - maxDuration
     - If exceedAmount <= (maxDuration * 0.5), include segment
     - Otherwise, stop processing (break)
4. Return subset of segments

**Use Cases:**
- Testing live streaming behavior with shorter content loops
- Quick iteration during development without processing full-length content
- Reducing memory footprint by limiting segment metadata
- Creating demo streams with short repeating clips

**Example:**
```bash
# 30-minute source playlist, loop after 10 seconds
encodersim --loop-after 10s https://example.com/playlist.m3u8

# Master playlist with 5-minute variants, loop after 30 seconds
encodersim --loop-after 30s https://example.com/master.m3u8
```

## Non-Requirements

The following are explicitly OUT OF SCOPE:

- Segment downloading or caching
- Segment transcoding or manipulation
- DVR/seeking functionality
- Authentication/authorization
- HTTPS server (only HTTP)
- Multiple simultaneous streams
- Dynamic segment insertion
- Ad insertion
- Analytics/metrics collection
- Web UI
- Configuration files

## References

- [HLS RFC 8216](https://datatracker.ietf.org/doc/html/rfc8216)
- [Google Go Style Guide](https://google.github.io/styleguide/go)
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

## Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0.0 | 2026-01-17 | Claude Sonnet 4.5 | Initial specification |
