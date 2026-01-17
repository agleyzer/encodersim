# EncoderSim - HLS Live Looping Tool

EncoderSim is a command-line tool that converts static HLS playlists into continuously looping live HLS feeds. It creates a live stream by presenting a sliding window of segments from a static playlist, looping infinitely.

## Features

- Converts static HLS (m3u8) playlists into live streaming feeds
- Sliding window approach for realistic live streaming behavior
- Configurable window size and server port
- No segment downloading - uses original segment URLs
- Health check endpoint for monitoring
- Graceful shutdown support
- Clean, simple architecture

## How It Works

1. Fetches and parses a static HLS playlist from a URL
2. Extracts segment information (URLs, durations)
3. Creates a sliding window over the segments
4. Serves a live HLS playlist that updates periodically
5. Loops back to the beginning when reaching the end
6. Clients fetch segments directly from the original source

The tool never downloads or caches video segments - it only manipulates the HLS manifest files to create the live streaming effect.

## Installation

### Prerequisites

- Go 1.21 or higher

### Build from Source

```bash
git clone https://github.com/agleyzer/encodersim.git
cd encodersim
go build -o encodersim ./cmd/encodersim
```

## Usage

### Basic Usage

```bash
encodersim https://example.com/playlist.m3u8
```

This starts a live HLS server on port 8080 with a 6-segment sliding window.

### Custom Configuration

```bash
encodersim --port 8080 --window-size 10 https://example.com/playlist.m3u8
```

### Command-Line Options

```
Options:
  -port int
        HTTP server port (default 8080)
  -window-size int
        Number of segments in sliding window (default 6)
  -verbose
        Enable verbose logging
  -version
        Show version and exit
```

### Accessing the Stream

Once running, you can access:

- **Live Playlist**: `http://localhost:8080/playlist.m3u8`
- **Health Check**: `http://localhost:8080/health`

### Example with VLC

```bash
# Start the server
encodersim https://example.com/static-playlist.m3u8

# In another terminal, play with VLC
vlc http://localhost:8080/playlist.m3u8
```

### Example with ffplay

```bash
ffplay http://localhost:8080/playlist.m3u8
```

## How the Sliding Window Works

Given a static playlist with 30 segments and a window size of 6:

```
T=0s:   Serve segments [0,1,2,3,4,5], sequence=0
T=10s:  Serve segments [1,2,3,4,5,6], sequence=1
T=20s:  Serve segments [2,3,4,5,6,7], sequence=2
...
T=290s: Serve segments [29,0,1,2,3,4], sequence=29 (looped back)
T=300s: Serve segments [0,1,2,3,4,5], sequence=30 (continues infinitely)
```

The window advances based on the `EXT-X-TARGETDURATION` from the source playlist.

## Health Check

The `/health` endpoint returns JSON with current statistics:

```bash
curl http://localhost:8080/health
```

Response:

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

## Architecture

The tool is organized into the following components:

- **Parser**: Fetches and parses HLS playlists, resolves URLs
- **Playlist Generator**: Manages sliding window and generates live playlists
- **HTTP Server**: Serves the live playlist and health endpoint
- **Main**: CLI parsing and component orchestration

## HLS Compliance

The generated playlists follow the HLS specification:

- `#EXT-X-VERSION:3` - HLS protocol version
- `#EXT-X-TARGETDURATION` - Maximum segment duration
- `#EXT-X-MEDIA-SEQUENCE` - Incrementing sequence number
- No `#EXT-X-ENDLIST` tag (indicates live stream)
- Proper segment duration tags (`#EXTINF`)

## Limitations

- Only supports media playlists (not master playlists with multiple bitrates)
- Segments must be accessible from client network
- No DVR or seeking backwards in time
- No authentication for segment URLs

## Development

### Project Structure

```
encodersim/
├── cmd/encodersim/          # Main application entry point
├── internal/                # Private implementation packages
│   ├── parser/             # HLS playlist parsing
│   ├── playlist/           # Live playlist generation
│   ├── server/             # HTTP server
│   └── config/             # Configuration types
└── pkg/segment/            # Shared segment data structures
```

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o encodersim ./cmd/encodersim
```

## License

MIT License - see LICENSE file for details

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For issues and questions, please use the GitHub issue tracker.
