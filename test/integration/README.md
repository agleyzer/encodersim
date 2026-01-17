# Integration Tests for EncoderSim

This directory contains integration tests for EncoderSim that verify end-to-end behavior by running the actual binary.

## Overview

The integration test framework provides:

- **TestHarness**: Manages the test environment (HTTP server + encodersim binary)
- **HTTP Server**: Serves test playlists from temporary directories
- **Process Management**: Starts/stops encodersim binary with configurable parameters
- **Playlist Parsing**: Utilities to parse and verify HLS playlist content
- **Wait Conditions**: Helper for polling until expected conditions are met

## Prerequisites

Before running integration tests, build the encodersim binary:

```bash
go build -o encodersim ./cmd/encodersim
```

## Running Tests

```bash
# Run all integration tests
go test ./test/integration

# Run with verbose output
go test -v ./test/integration

# Run specific test
go test -v ./test/integration -run TestWrappingPlaylist

# Skip integration tests in short mode
go test -short ./...
```

## Test Structure

### TestHarness

The `TestHarness` type manages the complete test environment:

```go
harness := NewTestHarness(t)
defer harness.Cleanup()

// Start HTTP server with test playlist
harness.StartHTTPServer(playlistContent, "test.m3u8")

// Start encodersim pointing to test server
harness.StartEncoderSim("test.m3u8", windowSize)

// Fetch current playlist from encodersim
playlist := harness.FetchPlaylist()

// Parse playlist into structured format
parsed := ParsePlaylist(playlist)

// Wait for a condition to be met
harness.WaitForCondition(func() bool {
    // Return true when condition is met
    return someCheck()
}, timeout, "description of condition")
```

### ParsedPlaylist

The `ParsePlaylist()` function returns a structured representation:

```go
type ParsedPlaylist struct {
    Version        int
    TargetDuration int
    MediaSequence  uint64
    Segments       []PlaylistSegment
    HasEndList     bool
}

type PlaylistSegment struct {
    Duration      float64
    URL           string
    Discontinuity bool
}
```

## Existing Tests

### TestWrappingPlaylist

Verifies that:
1. Initial playlist has correct window size and segments
2. Playlist is marked as live (no `#EXT-X-ENDLIST`)
3. Window advances over time
4. Discontinuity tag is inserted when playlist loops back to start
5. Discontinuity is placed before the correct segment
6. Stream continues operating correctly after wrapping

**Test Parameters:**
- 5 segments, 1 second each
- Window size: 3
- Expected wrap at sequence 3: `[segment003, segment004, segment000]`

## Adding New Tests

### Example: Testing Different Window Sizes

```go
func TestSmallWindow(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    harness := NewTestHarness(t)
    defer harness.Cleanup()

    // Create playlist with 10 segments
    testPlaylist := createTestPlaylist(10, 2.0)

    harness.StartHTTPServer(testPlaylist, "test.m3u8")
    harness.StartEncoderSim("test.m3u8", 2) // window size = 2

    // Fetch and verify
    playlist := harness.FetchPlaylist()
    parsed := ParsePlaylist(playlist)

    if len(parsed.Segments) != 2 {
        t.Errorf("expected 2 segments, got %d", len(parsed.Segments))
    }
}
```

### Example: Testing Health Endpoint

```go
func TestHealthEndpoint(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    harness := NewTestHarness(t)
    defer harness.Cleanup()

    testPlaylist := createTestPlaylist(5, 1.0)
    harness.StartHTTPServer(testPlaylist, "test.m3u8")
    harness.StartEncoderSim("test.m3u8", 3)

    // Fetch health endpoint
    health := harness.FetchHealth()

    // Parse JSON and verify stats
    // (add JSON parsing as needed)
    if !strings.Contains(health, `"status":"ok"`) {
        t.Error("health endpoint should return ok status")
    }
}
```

### Example: Testing Rapid Polling

```go
func TestConcurrentRequests(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    harness := NewTestHarness(t)
    defer harness.Cleanup()

    testPlaylist := createTestPlaylist(20, 1.0)
    harness.StartHTTPServer(testPlaylist, "test.m3u8")
    harness.StartEncoderSim("test.m3u8", 6)

    // Make 100 concurrent requests
    var wg sync.WaitGroup
    errors := make(chan error, 100)

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            playlist := harness.FetchPlaylist()
            if len(playlist) == 0 {
                errors <- fmt.Errorf("received empty playlist")
            }
        }()
    }

    wg.Wait()
    close(errors)

    for err := range errors {
        t.Error(err)
    }
}
```

## Helper Functions

### createTestPlaylist(numSegments int, duration float64) string

Creates a simple HLS playlist with specified number of segments.

### padNumber(num, width int) string

Pads a number with leading zeros (e.g., `padNumber(5, 3)` returns `"005"`).

## Best Practices

1. **Always use `testing.Short()` check**: Skip integration tests when `-short` flag is used
2. **Always defer `Cleanup()`**: Ensure processes are stopped even if test fails
3. **Use meaningful test names**: Follow `TestXxx` pattern describing what is tested
4. **Add test phases**: Break complex tests into logical phases with logging
5. **Use `WaitForCondition`**: Don't use `time.Sleep()` - poll for expected conditions
6. **Verify incrementally**: Check conditions as you go, don't wait until end

## Debugging

### Verbose Output

Run with `-v` flag to see detailed logging from both the test and encodersim:

```bash
go test -v ./test/integration
```

### Check encodersim Output

The test harness captures stdout/stderr from encodersim. Look for `level=INFO` or `level=ERROR` messages in test output.

### Manually Inspect Playlist

Add temporary logging to see playlist content:

```go
playlist := harness.FetchPlaylist()
t.Logf("Current playlist:\n%s", playlist)
```

### Port Conflicts

The harness automatically finds available ports. If tests fail due to port issues, check for processes holding ports:

```bash
# Find processes on ports
lsof -i :8080
lsof -i :9000
```

## Troubleshooting

### "encodersim binary not found"

Build the binary before running tests:
```bash
go build -o encodersim ./cmd/encodersim
```

### "timeout waiting for condition"

- Increase timeout duration in `WaitForCondition`
- Check that encodersim is actually running (look for startup logs)
- Verify test playlist is valid HLS format
- Add debug logging to see what's being returned

### Test hangs indefinitely

- Ensure `defer harness.Cleanup()` is called
- Check for deadlocks in condition functions
- Verify context cancellation is working

## Future Test Ideas

- **Different segment durations**: Variable length segments
- **Large playlists**: 100+ segments
- **Edge cases**: Single segment, window size = total segments
- **HTTP errors**: Test playlist becomes unavailable
- **Malformed playlists**: Invalid m3u8 content
- **Rapid window advancement**: Short target durations (<1 second)
- **Multiple clients**: Simulate many concurrent viewers
- **Long-running stability**: Run for extended periods (hours)
