package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/raft"
)

// Manager manages a Raft cluster for distributed state synchronization.
type Manager struct {
	config    Config
	raft      *raft.Raft
	fsm       *PlaylistFSM
	transport *raft.NetworkTransport
	logger    *slog.Logger
	mu        sync.RWMutex
	shutdown  bool
}

// NewManager creates a new cluster manager.
func NewManager(config Config, logger *slog.Logger) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Manager{
		config:   config,
		fsm:      NewPlaylistFSM(logger),
		logger:   logger,
		shutdown: false,
	}, nil
}

// Start initializes and starts the Raft cluster.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.raft != nil {
		return fmt.Errorf("cluster already started")
	}

	// Create Raft configuration
	raftConfig := raft.DefaultConfig()
	// Use bind address as LocalID for consistency with bootstrap configuration
	raftConfig.LocalID = raft.ServerID(m.config.BindAddr)
	raftConfig.HeartbeatTimeout = m.config.HeartbeatTimeout
	raftConfig.ElectionTimeout = m.config.ElectionTimeout
	raftConfig.LeaderLeaseTimeout = m.config.HeartbeatTimeout
	raftConfig.SnapshotInterval = m.config.SnapshotInterval
	raftConfig.SnapshotThreshold = m.config.SnapshotThreshold

	// Use a no-op logger to avoid excessive Raft logging
	raftConfig.Logger = newNoOpHCLogger()

	// Create in-memory stores
	logStore := raft.NewInmemStore()
	stableStore := raft.NewInmemStore()
	snapshotStore := raft.NewInmemSnapshotStore()

	// Create network transport
	addr, err := net.ResolveTCPAddr("tcp", m.config.BindAddr)
	if err != nil {
		return fmt.Errorf("resolve bind address: %w", err)
	}

	transport, err := raft.NewTCPTransport(m.config.BindAddr, addr, 3, 10*time.Second, nil)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}
	m.transport = transport

	// Create Raft instance
	r, err := raft.NewRaft(raftConfig, m.fsm, logStore, stableStore, snapshotStore, transport)
	if err != nil {
		transport.Close()
		return fmt.Errorf("create raft: %w", err)
	}
	m.raft = r

	// Bootstrap cluster if this is the first node
	configuration := raft.Configuration{
		Servers: make([]raft.Server, 0, len(m.config.Peers)),
	}

	for _, peer := range m.config.Peers {
		// Use peer address as both ID and address for simplicity
		configuration.Servers = append(configuration.Servers, raft.Server{
			ID:       raft.ServerID(peer),
			Address:  raft.ServerAddress(peer),
			Suffrage: raft.Voter,
		})
	}

	// Bootstrap the cluster
	future := m.raft.BootstrapCluster(configuration)
	if err := future.Error(); err != nil && err != raft.ErrCantBootstrap {
		m.logger.Error("failed to bootstrap cluster", "error", err)
		// Continue anyway - node might be joining existing cluster
	}

	m.logger.Info("cluster started",
		"node_id", m.config.RaftID,
		"raft_id", m.config.BindAddr,
		"bind", m.config.BindAddr,
		"peers", len(m.config.Peers))

	return nil
}

// AdvanceWindow submits an AdvanceWindowCommand to the Raft cluster.
func (m *Manager) AdvanceWindow() error {
	m.mu.RLock()
	if m.shutdown {
		m.mu.RUnlock()
		return fmt.Errorf("cluster is shut down")
	}
	r := m.raft
	m.mu.RUnlock()

	if r == nil {
		return fmt.Errorf("cluster not started")
	}

	cmd := Command{
		Type: CommandAdvanceWindow,
		Data: AdvanceWindowCommand{VariantIndex: -1},
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		return fmt.Errorf("encode command: %w", err)
	}

	future := r.Apply(data, 5*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("apply command: %w", err)
	}

	return nil
}

// Initialize sets the initial FSM state.
func (m *Manager) Initialize(state ClusterState) error {
	m.mu.RLock()
	if m.shutdown {
		m.mu.RUnlock()
		return fmt.Errorf("cluster is shut down")
	}
	r := m.raft
	m.mu.RUnlock()

	if r == nil {
		return fmt.Errorf("cluster not started")
	}

	cmd := Command{
		Type: CommandInitialize,
		Data: InitializeCommand{State: state},
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		return fmt.Errorf("encode command: %w", err)
	}

	future := r.Apply(data, 5*time.Second)
	if err := future.Error(); err != nil {
		return fmt.Errorf("apply command: %w", err)
	}

	return nil
}

// GetState returns the current FSM state.
func (m *Manager) GetState() ClusterState {
	return m.fsm.GetState()
}

// IsLeader returns true if this node is the Raft leader.
func (m *Manager) IsLeader() bool {
	m.mu.RLock()
	r := m.raft
	m.mu.RUnlock()

	if r == nil {
		return false
	}

	return r.State() == raft.Leader
}

// LeaderAddr returns the address of the current Raft leader.
func (m *Manager) LeaderAddr() string {
	m.mu.RLock()
	r := m.raft
	m.mu.RUnlock()

	if r == nil {
		return ""
	}

	leaderAddr, _ := r.LeaderWithID()
	return string(leaderAddr)
}

// State returns the current Raft state.
func (m *Manager) State() string {
	m.mu.RLock()
	r := m.raft
	m.mu.RUnlock()

	if r == nil {
		return "NotStarted"
	}

	switch r.State() {
	case raft.Follower:
		return "Follower"
	case raft.Candidate:
		return "Candidate"
	case raft.Leader:
		return "Leader"
	case raft.Shutdown:
		return "Shutdown"
	default:
		return "Unknown"
	}
}

// Peers returns the list of peer addresses.
func (m *Manager) Peers() []string {
	return m.config.Peers
}

// NodeID returns this node's Raft ID.
func (m *Manager) NodeID() string {
	return m.config.RaftID
}

// Shutdown gracefully shuts down the Raft cluster.
func (m *Manager) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shutdown {
		return nil
	}

	m.shutdown = true

	if m.raft != nil {
		if err := m.raft.Shutdown().Error(); err != nil {
			m.logger.Error("failed to shutdown raft", "error", err)
			return fmt.Errorf("shutdown raft: %w", err)
		}
	}

	if m.transport != nil {
		if err := m.transport.Close(); err != nil {
			m.logger.Error("failed to close transport", "error", err)
			return fmt.Errorf("close transport: %w", err)
		}
	}

	m.logger.Info("cluster shut down")
	return nil
}

// WaitForLeader blocks until a leader is elected or context is canceled.
func (m *Manager) WaitForLeader(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.LeaderAddr() != "" {
				return nil
			}
		}
	}
}
