#!/bin/bash
# Stop all encodersim cluster instances

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Stopping encodersim cluster...${NC}"

# Kill all encodersim processes running in cluster mode
if pkill -f "encodersim.*--cluster"; then
    echo -e "${GREEN}Cluster processes terminated${NC}"
else
    echo -e "${YELLOW}No cluster processes found${NC}"
fi

# Clean up log files if they exist
if ls node*.log >/dev/null 2>&1; then
    echo -e "${YELLOW}Cleaning up log files...${NC}"
    rm -f node*.log
    echo -e "${GREEN}Log files removed${NC}"
fi

echo -e "${GREEN}Done${NC}"
