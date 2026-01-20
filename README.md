# EncoderSim - HLS Live Looping Tool

EncoderSim is a command-line tool that converts static HLS playlists into continuously looping live HLS feeds. It creates a live stream by presenting a sliding window of segments from a static playlist, looping infinitely.

## Features

- Converts static HLS (m3u8) playlists into live streaming feeds
- Sliding window approach for realistic live streaming behavior
- Configurable window size and server port
- No segment downloading - uses original segment URLs
- Health check endpoint for monitoring
- Graceful shutdown support
- **Cluster mode** with Raft consensus for high availability and load balancing
- Multi-bitrate (master playlist) support
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

### URL Structure

All playlists (both master and single media) are served with the same URL structure:
- **Master Playlist**: `http://localhost:8080/playlist.m3u8`
- **Variant Playlists**: `http://localhost:8080/variant/0/playlist.m3u8`, `/variant/1/playlist.m3u8`, etc.

Single media playlists are automatically wrapped as a single variant (variant 0).

### Master Playlist Support

EncoderSim supports multi-bitrate (master) playlists:

```bash
encodersim https://example.com/master.m3u8
```

The tool auto-detects master playlists and serves all variants. Each variant maintains its own sliding window and advances based on the maximum target duration across variants for synchronization.

### Limiting Content Duration

Use the `--loop-after` flag to limit the amount of content used from the source playlist:

```bash
# Loop after 10 seconds of content
encodersim --loop-after 10s https://example.com/playlist.m3u8

# Loop after 1 minute 30 seconds
encodersim --loop-after 1m30s https://example.com/playlist.m3u8

# Works with master playlists too
encodersim --loop-after 30s https://example.com/master.m3u8
```

The tool will include segments up to the specified duration (at segment boundaries), allowing up to 50% overage to avoid cutting off mid-segment. This is useful for testing live streaming behavior with shorter content loops.

### Cluster Mode (High Availability)

EncoderSim supports running multiple instances in a cluster for high availability and load balancing. All instances serve identical playlists at the same time using Raft consensus.

#### Running a 3-Node Cluster

```bash
# Node 1
./encodersim --cluster \
  --raft-id=node1 \
  --raft-bind=10.0.0.1:9000 \
  --peers=10.0.0.1:9000,10.0.0.2:9000,10.0.0.3:9000 \
  --port=8080 \
  https://example.com/playlist.m3u8

# Node 2
./encodersim --cluster \
  --raft-id=node2 \
  --raft-bind=10.0.0.2:9000 \
  --peers=10.0.0.1:9000,10.0.0.2:9000,10.0.0.3:9000 \
  --port=8080 \
  https://example.com/playlist.m3u8

# Node 3
./encodersim --cluster \
  --raft-id=node3 \
  --raft-bind=10.0.0.3:9000 \
  --peers=10.0.0.1:9000,10.0.0.2:9000,10.0.0.3:9000 \
  --port=8080 \
  https://example.com/playlist.m3u8
```

#### How Cluster Mode Works

- **Raft Consensus**: One node is elected as the leader, which advances the sliding window
- **State Replication**: Window position and sequence numbers are replicated to all nodes
- **Identical Playlists**: All nodes serve the exact same playlist at any given moment
- **Automatic Failover**: If the leader fails, a new leader is automatically elected
- **Load Balancing**: Place a load balancer (nginx, HAProxy) in front of the cluster

#### Cluster Mode Flags

```
-cluster
      Enable cluster mode with Raft consensus
-raft-id string
      Unique Raft node ID (required for cluster mode)
-raft-bind string
      Raft bind address for inter-node communication (host:port, required for cluster mode)
-peers string
      Comma-separated list of all peer Raft addresses including this node (required for cluster mode)
```

#### Checking Cluster Status

```bash
# Check cluster status
curl http://localhost:8080/cluster/status

# Example response
{
  "cluster_enabled": true,
  "is_leader": false,
  "leader_address": "10.0.0.1:9000",
  "raft_state": "Follower"
}
```

#### Deploying Behind a Load Balancer

**Nginx Example:**

```nginx
upstream encodersim {
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
    server 10.0.0.3:8080;
}

server {
    listen 80;
    location / {
        proxy_pass http://encodersim;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
```

**HAProxy Example:**

```
backend encodersim
    balance roundrobin
    server node1 10.0.0.1:8080 check
    server node2 10.0.0.2:8080 check
    server node3 10.0.0.3:8080 check
```

### Command-Line Options

```
Options:
  -port int
        HTTP server port (default 8080)
  -window-size int
        Number of segments in sliding window (default 6)
  -loop-after duration
        Maximum duration of content to use before looping (e.g., '10s', '1m30s')
        Uses all segments if not specified
  -master
        Expect master playlist with multiple variants (auto-detected if not set)
  -variants string
        Comma-separated list of variant indices to serve (e.g., '0,2,4')
        Serves all variants if not specified
  -cluster
        Enable cluster mode with Raft consensus
  -raft-id string
        Unique Raft node ID (required for cluster mode)
  -raft-bind string
        Raft bind address for inter-node communication (host:port, required for cluster mode)
  -peers string
        Comma-separated list of all peer Raft addresses including this node (required for cluster mode)
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

**Response** (includes per-variant details):

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

**Cluster Mode Response** (adds cluster information):

```json
{
  "status": "ok",
  "stats": {
    "is_master": true,
    "cluster_mode": true,
    "is_leader": false,
    "leader_address": "10.0.0.1:9000",
    "raft_state": "Follower",
    "window_size": 6,
    "sequence_number": 42,
    "target_duration": 10,
    "variant_count": 1,
    "variants": [...]
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

- Segments must be accessible from client network
- No DVR or seeking backwards in time
- No authentication for segment URLs
- Variants with different segment counts may have minor sync differences when looping

## Development

### Project Structure

```
encodersim/
├── cmd/encodersim/          # Main application entry point
├── internal/                # Private implementation packages
│   ├── parser/             # HLS playlist parsing (master & media)
│   ├── playlist/           # Live playlist generation
│   ├── server/             # HTTP server & routing
│   ├── segment/            # Segment data structures
│   └── variant/            # Variant stream data structures
└── test/                   # Test resources and scripts
    ├── integration/        # Integration tests
    └── test.sh             # Manual testing script
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
