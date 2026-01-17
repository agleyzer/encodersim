# EncoderSim - Technical Specification

## Overview

EncoderSim is a Go command-line tool that converts static HLS (HTTP Live Streaming) playlists into continuously looping live HLS feeds. The tool manipulates only the HLS manifest files (m3u8) without downloading or caching video segments, presenting a sliding window of segments that loops infinitely.

**Version:** 1.0.0
**Language:** Go 1.21+
**License:** MIT

## Project Goals

1. Convert static VOD (Video on Demand) HLS playlists into live streaming feeds
2. Create realistic live streaming behavior using a sliding window approach
3. Loop content infinitely without user intervention
4. Maintain HLS specification compliance
5. Provide a simple, easy-to-use CLI interface

## Core Requirements

### 1. Input Requirements

**MUST:**
- Accept a URL to a static HLS media playlist (m3u8) as input
- Support both HTTP and HTTPS URLs
- Parse HLS version 3+ playlists
- Handle both relative and absolute segment URLs
- Resolve relative segment URLs to absolute URLs based on playlist location

**MUST NOT:**
- Accept master playlists (multi-bitrate) - only media playlists
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
  - `stats`: object with:
    - `total_segments`: Total segments in source playlist
    - `window_size`: Current window size
    - `current_position`: Current position in source playlist
    - `sequence_number`: Current media sequence number
    - `target_duration`: Target duration in seconds

**Example Response:**
```json
{
  "status": "ok",
  "stats": {
    "total_segments": 30,
    "window_size": 6,
    "current_position": 12,
    "sequence_number": 42,
    "target_duration": 10
  }
}
```

### 8. Command-Line Interface

**MUST:**
- Accept playlist URL as positional argument
- Support flags:
  - `--port <int>`: HTTP server port (default: 8080, range: 1-65535)
  - `--window-size <int>`: Sliding window size (default: 6, minimum: 1)
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
  - Master playlists (instead of media playlists)
  - Empty playlists
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

**MUST:**
- Follow standard Go project layout:
  ```
  encodersim/
  ├── cmd/encodersim/         # Main application
  ├── internal/               # Private packages
  │   ├── parser/            # HLS parsing
  │   ├── playlist/          # Live playlist generation
  │   └── server/            # HTTP server
  ├── pkg/                   # Public packages
  │   └── segment/           # Segment data structures
  └── test/                  # Test resources
  ```
- Use `internal/` for private packages (enforced by Go compiler)
- Use `pkg/` for potentially reusable packages
- One package per directory

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

## Non-Requirements

The following are explicitly OUT OF SCOPE:

- Multi-bitrate support (master playlists)
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
