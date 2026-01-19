#!/bin/bash
# Start a 3-node encodersim cluster on localhost

set -e

# Configuration
PLAYLIST_URL="https://demo.unified-streaming.com/k8s/features/stable/video/tears-of-steel/tears-of-steel.ism/.m3u8"
LOOP_AFTER="1m"

# Ports
HTTP_PORT_1=8001
HTTP_PORT_2=8002
HTTP_PORT_3=8003

RAFT_PORT_1=9001
RAFT_PORT_2=9002
RAFT_PORT_3=9003

# Peers list (all raft addresses)
PEERS="127.0.0.1:$RAFT_PORT_1,127.0.0.1:$RAFT_PORT_2,127.0.0.1:$RAFT_PORT_3"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if binary exists
if [ ! -f "./encodersim" ]; then
    echo -e "${RED}Error: encodersim binary not found${NC}"
    echo "Run 'go build -o encodersim ./cmd/encodersim' first"
    exit 1
fi

# Function to check if port is in use
check_port() {
    local port=$1
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        echo -e "${RED}Error: Port $port is already in use${NC}"
        exit 1
    fi
}

# Check all ports are available
echo "Checking ports availability..."
check_port $HTTP_PORT_1
check_port $HTTP_PORT_2
check_port $HTTP_PORT_3
check_port $RAFT_PORT_1
check_port $RAFT_PORT_2
check_port $RAFT_PORT_3
echo -e "${GREEN}All ports available${NC}"

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Shutting down cluster...${NC}"
    pkill -f "encodersim.*--cluster" || true
    echo -e "${GREEN}Cluster stopped${NC}"
}

# Set trap to cleanup on exit
trap cleanup EXIT INT TERM

echo -e "${GREEN}Starting 3-node encodersim cluster${NC}"
echo "Playlist: $PLAYLIST_URL"
echo "Loop after: $LOOP_AFTER"
echo ""

# Start node 1
echo -e "${YELLOW}Starting node1 (HTTP: $HTTP_PORT_1, Raft: $RAFT_PORT_1)${NC}"
./encodersim \
    --cluster \
    --raft-id=node1 \
    --raft-bind=127.0.0.1:$RAFT_PORT_1 \
    --peers=$PEERS \
    --port=$HTTP_PORT_1 \
    --loop-after=$LOOP_AFTER \
    --window-size=6 \
    "$PLAYLIST_URL" > node1.log 2>&1 &
NODE1_PID=$!
echo "  PID: $NODE1_PID"
echo "  Logs: node1.log"

# Start node 2
echo -e "${YELLOW}Starting node2 (HTTP: $HTTP_PORT_2, Raft: $RAFT_PORT_2)${NC}"
./encodersim \
    --cluster \
    --raft-id=node2 \
    --raft-bind=127.0.0.1:$RAFT_PORT_2 \
    --peers=$PEERS \
    --port=$HTTP_PORT_2 \
    --loop-after=$LOOP_AFTER \
    --window-size=6 \
    "$PLAYLIST_URL" > node2.log 2>&1 &
NODE2_PID=$!
echo "  PID: $NODE2_PID"
echo "  Logs: node2.log"

# Start node 3
echo -e "${YELLOW}Starting node3 (HTTP: $HTTP_PORT_3, Raft: $RAFT_PORT_3)${NC}"
./encodersim \
    --cluster \
    --raft-id=node3 \
    --raft-bind=127.0.0.1:$RAFT_PORT_3 \
    --peers=$PEERS \
    --port=$HTTP_PORT_3 \
    --loop-after=$LOOP_AFTER \
    --window-size=6 \
    "$PLAYLIST_URL" > node3.log 2>&1 &
NODE3_PID=$!
echo "  PID: $NODE3_PID"
echo "  Logs: node3.log"

echo ""
echo -e "${GREEN}Cluster starting...${NC}"
echo "Waiting for nodes to initialize..."
sleep 5

# Check cluster status
echo ""
echo -e "${GREEN}Cluster Status:${NC}"
for port in $HTTP_PORT_1 $HTTP_PORT_2 $HTTP_PORT_3; do
    echo ""
    echo "Node on port $port:"
    if curl -s http://localhost:$port/cluster/status 2>/dev/null | jq . 2>/dev/null; then
        :
    else
        echo "  (node still initializing...)"
    fi
done

echo ""
echo -e "${GREEN}Cluster is running!${NC}"
echo ""
echo "Access points:"
echo "  Node 1: http://localhost:$HTTP_PORT_1/playlist.m3u8"
echo "  Node 2: http://localhost:$HTTP_PORT_2/playlist.m3u8"
echo "  Node 3: http://localhost:$HTTP_PORT_3/playlist.m3u8"
echo ""
echo "Health checks:"
echo "  Node 1: http://localhost:$HTTP_PORT_1/health"
echo "  Node 2: http://localhost:$HTTP_PORT_2/health"
echo "  Node 3: http://localhost:$HTTP_PORT_3/health"
echo ""
echo "Cluster status:"
echo "  Node 1: http://localhost:$HTTP_PORT_1/cluster/status"
echo "  Node 2: http://localhost:$HTTP_PORT_2/cluster/status"
echo "  Node 3: http://localhost:$HTTP_PORT_3/cluster/status"
echo ""
echo "Logs: node1.log, node2.log, node3.log"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop the cluster${NC}"

wait $NODE1_PID $NODE2_PID $NODE3_PID

echo -e "${RED}All done${NC}"
