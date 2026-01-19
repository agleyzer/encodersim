package cluster

import (
	"fmt"
	"net"
	"time"
)

// Config holds the configuration for a cluster node.
type Config struct {
	// RaftID is the unique identifier for this Raft node.
	RaftID string
	// BindAddr is the address to bind for Raft communication (host:port).
	BindAddr string
	// Peers is the list of peer Raft addresses (including this node).
	Peers []string
	// HeartbeatTimeout is the Raft heartbeat timeout.
	HeartbeatTimeout time.Duration
	// ElectionTimeout is the Raft election timeout.
	ElectionTimeout time.Duration
	// SnapshotInterval is how often to take snapshots.
	SnapshotInterval time.Duration
	// SnapshotThreshold is the number of logs before taking a snapshot.
	SnapshotThreshold uint64
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.RaftID == "" {
		return fmt.Errorf("raft-id is required")
	}

	if c.BindAddr == "" {
		return fmt.Errorf("raft-bind is required")
	}

	if _, _, err := net.SplitHostPort(c.BindAddr); err != nil {
		return fmt.Errorf("invalid raft-bind address %q: %w", c.BindAddr, err)
	}

	if len(c.Peers) == 0 {
		return fmt.Errorf("at least one peer is required")
	}

	for i, peer := range c.Peers {
		if _, _, err := net.SplitHostPort(peer); err != nil {
			return fmt.Errorf("invalid peer address %d %q: %w", i, peer, err)
		}
	}

	// Set defaults
	if c.HeartbeatTimeout == 0 {
		c.HeartbeatTimeout = 1 * time.Second
	}
	if c.ElectionTimeout == 0 {
		c.ElectionTimeout = 1 * time.Second
	}
	if c.SnapshotInterval == 0 {
		c.SnapshotInterval = 120 * time.Second
	}
	if c.SnapshotThreshold == 0 {
		c.SnapshotThreshold = 8192
	}

	return nil
}
