#!/bin/bash

# Test script for EncoderSim
# Starts a local HTTP server with test HLS playlist and runs encodersim
#
# Usage: ./test.sh [playlist-file]
#   playlist-file: Path to HLS playlist file (default: playlist.m3u8)

set -e

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$TEST_DIR/.." && pwd)"
TEST_PORT=9000
ENCODERSIM_PORT=8080
PLAYLIST_FILE="${1:-playlist.m3u8}"
HOSTNAME=$(hostname --long)

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== EncoderSim Test Script ===${NC}"

# Check if playlist file exists
if [ ! -f "$TEST_DIR/$PLAYLIST_FILE" ]; then
    echo -e "${RED}Error: Playlist file not found: $TEST_DIR/$PLAYLIST_FILE${NC}"
    echo "Usage: $0 [playlist-file]"
    echo "  playlist-file: Path to HLS playlist file (default: playlist.m3u8)"
    exit 1
fi

echo -e "${GREEN}Using playlist: $PLAYLIST_FILE${NC}"

# Check if encodersim binary exists
if [ ! -f "$PROJECT_DIR/encodersim" ]; then
    echo -e "${YELLOW}Building encodersim...${NC}"
    cd "$PROJECT_DIR"
    go build -o encodersim ./cmd/encodersim
    cd "$TEST_DIR"
fi

# Function to cleanup on exit
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    if [ ! -z "$HTTP_PID" ]; then
        kill $HTTP_PID 2>/dev/null || true
    fi
    if [ ! -z "$ENCODERSIM_PID" ]; then
        kill $ENCODERSIM_PID 2>/dev/null || true
    fi
    exit 0
}

trap cleanup INT TERM EXIT

# Start HTTP server in background
echo -e "${GREEN}Starting HTTP server on port $TEST_PORT...${NC}"
cd "$TEST_DIR" || exit 1
python3 -m http.server $TEST_PORT > /tmp/http_server.log 2>&1 &
HTTP_PID=$!
cd "$PROJECT_DIR"

# Wait for HTTP server to be ready
echo "Waiting for HTTP server to start..."
sleep 2

# Check if HTTP server is running
if ! kill -0 $HTTP_PID 2>/dev/null; then
    echo "Error: HTTP server failed to start"
    echo "Check /tmp/http_server.log for details"
    cat /tmp/http_server.log
    exit 1
fi

echo -e "${GREEN}HTTP server running (PID: $HTTP_PID)${NC}"
echo -e "${BLUE}Test playlist available at: http://$HOSTNAME:$TEST_PORT/$PLAYLIST_FILE${NC}"

# Start encodersim
echo -e "\n${GREEN}Starting encodersim on port $ENCODERSIM_PORT...${NC}"
./encodersim --verbose --window-size 3 "http://localhost:$TEST_PORT/$PLAYLIST_FILE" &
ENCODERSIM_PID=$!

# Wait for encodersim to start
sleep 2

# Check if encodersim is running
if ! kill -0 $ENCODERSIM_PID 2>/dev/null; then
    echo "Error: encodersim failed to start"
    exit 1
fi

echo -e "${GREEN}EncoderSim running (PID: $ENCODERSIM_PID)${NC}"
echo -e "${BLUE}Live HLS stream available at: http://$HOSTNAME:$ENCODERSIM_PORT/playlist.m3u8${NC}"

# Display usage instructions
echo -e "\n${YELLOW}=== Testing Instructions ===${NC}"
echo "1. Open the live stream in a player:"
echo -e "   ${BLUE}vlc http://$HOSTNAME:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo -e "   ${BLUE}ffplay http://$HOSTNAME:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo ""
echo "2. Check the current playlist:"
echo -e "   ${BLUE}curl http://$HOSTNAME:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo ""
echo "3. Check health status:"
echo -e "   ${BLUE}curl http://$HOSTNAME:$ENCODERSIM_PORT/health${NC}"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop the test${NC}"

# Wait for user to stop
wait $ENCODERSIM_PID
