#!/bin/bash

# Test script for EncoderSim
# Starts a local HTTP server with test HLS playlist and runs encodersim

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$SCRIPT_DIR/test"
TEST_PORT=9000
ENCODERSIM_PORT=8080

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== EncoderSim Test Script ===${NC}"

# Check if encodersim binary exists
if [ ! -f "$SCRIPT_DIR/encodersim" ]; then
    echo -e "${YELLOW}Building encodersim...${NC}"
    go build -o encodersim ./cmd/encodersim
fi

# Create test directory first
mkdir -p "$TEST_DIR"

# Create test playlist if it doesn't exist
if [ ! -f "$TEST_DIR/playlist.m3u8" ]; then
    echo -e "${YELLOW}Creating test playlist...${NC}"
    cat > "$TEST_DIR/playlist.m3u8" << 'EOF'
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:9.9,
https://example.com/segment001.ts
#EXTINF:10.0,
https://example.com/segment002.ts
#EXTINF:10.1,
https://example.com/segment003.ts
#EXTINF:9.8,
https://example.com/segment004.ts
#EXTINF:10.0,
https://example.com/segment005.ts
#EXTINF:10.2,
https://example.com/segment006.ts
#EXTINF:9.9,
https://example.com/segment007.ts
#EXTINF:10.0,
https://example.com/segment008.ts
#EXT-X-ENDLIST
EOF
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
cd "$SCRIPT_DIR"

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
echo -e "${BLUE}Test playlist available at: http://localhost:$TEST_PORT/playlist.m3u8${NC}"

# Start encodersim
echo -e "\n${GREEN}Starting encodersim on port $ENCODERSIM_PORT...${NC}"
./encodersim --verbose --window-size 3 "http://localhost:$TEST_PORT/playlist.m3u8" &
ENCODERSIM_PID=$!

# Wait for encodersim to start
sleep 2

# Check if encodersim is running
if ! kill -0 $ENCODERSIM_PID 2>/dev/null; then
    echo "Error: encodersim failed to start"
    exit 1
fi

echo -e "${GREEN}EncoderSim running (PID: $ENCODERSIM_PID)${NC}"
echo -e "${BLUE}Live HLS stream available at: http://localhost:$ENCODERSIM_PORT/playlist.m3u8${NC}"

# Display usage instructions
echo -e "\n${YELLOW}=== Testing Instructions ===${NC}"
echo "1. Open the live stream in a player:"
echo -e "   ${BLUE}vlc http://localhost:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo -e "   ${BLUE}ffplay http://localhost:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo ""
echo "2. Check the current playlist:"
echo -e "   ${BLUE}curl http://localhost:$ENCODERSIM_PORT/playlist.m3u8${NC}"
echo ""
echo "3. Check health status:"
echo -e "   ${BLUE}curl http://localhost:$ENCODERSIM_PORT/health${NC}"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop the test${NC}"

# Wait for user to stop
wait $ENCODERSIM_PID
