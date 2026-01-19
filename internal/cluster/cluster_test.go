package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestManager_NewManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				RaftID:   "node1",
				BindAddr: "127.0.0.1:9000",
				Peers:    []string{"127.0.0.1:9000"},
			},
			wantErr: false,
		},
		{
			name: "missing raft-id",
			config: Config{
				BindAddr: "127.0.0.1:9000",
				Peers:    []string{"127.0.0.1:9000"},
			},
			wantErr: true,
		},
		{
			name: "missing bind-addr",
			config: Config{
				RaftID: "node1",
				Peers:  []string{"127.0.0.1:9000"},
			},
			wantErr: true,
		},
		{
			name: "missing peers",
			config: Config{
				RaftID:   "node1",
				BindAddr: "127.0.0.1:9000",
			},
			wantErr: true,
		},
		{
			name: "invalid bind-addr",
			config: Config{
				RaftID:   "node1",
				BindAddr: "invalid",
				Peers:    []string{"127.0.0.1:9000"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewManager(tt.config, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_StartAndShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config := Config{
		RaftID:            "node1",
		BindAddr:          "127.0.0.1:0", // Use port 0 for auto-assignment
		Peers:             []string{"127.0.0.1:0"},
		HeartbeatTimeout:  100 * time.Millisecond,
		ElectionTimeout:   100 * time.Millisecond,
		SnapshotInterval:  1 * time.Hour,
		SnapshotThreshold: 10000,
	}

	manager, err := NewManager(config, logger)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify manager is running
	if manager.State() == "NotStarted" {
		t.Error("Manager should be started")
	}

	// Shutdown
	if err := manager.Shutdown(); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Verify shutdown is idempotent
	if err := manager.Shutdown(); err != nil {
		t.Errorf("Second Shutdown() error = %v", err)
	}
}

func TestManager_InitializeAndGetState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a single-node cluster
	manager := createTestCluster(t, logger, 1)[0]
	defer manager.Shutdown()

	// Wait for leader election
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.WaitForLeader(ctx); err != nil {
		t.Fatalf("WaitForLeader() error = %v", err)
	}

	// Initialize state
	initialState := ClusterState{
		CurrentPosition: 5,
		SequenceNumber:  42,
		TotalSegments:   10,
	}

	if err := manager.Initialize(initialState); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Give Raft time to apply the command
	time.Sleep(200 * time.Millisecond)

	// Get state
	state := manager.GetState()
	if state.CurrentPosition != 5 {
		t.Errorf("CurrentPosition = %d, want 5", state.CurrentPosition)
	}
	if state.SequenceNumber != 42 {
		t.Errorf("SequenceNumber = %d, want 42", state.SequenceNumber)
	}
	if state.TotalSegments != 10 {
		t.Errorf("TotalSegments = %d, want 10", state.TotalSegments)
	}
}

func TestManager_AdvanceWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a single-node cluster
	manager := createTestCluster(t, logger, 1)[0]
	defer manager.Shutdown()

	// Wait for leader election
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.WaitForLeader(ctx); err != nil {
		t.Fatalf("WaitForLeader() error = %v", err)
	}

	// Initialize state
	initialState := ClusterState{
		CurrentPosition: 0,
		SequenceNumber:  0,
		TotalSegments:   5,
	}
	if err := manager.Initialize(initialState); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Advance window
	if err := manager.AdvanceWindow(); err != nil {
		t.Fatalf("AdvanceWindow() error = %v", err)
	}

	// Give Raft time to apply the command
	time.Sleep(200 * time.Millisecond)

	// Verify state
	state := manager.GetState()
	if state.CurrentPosition != 1 {
		t.Errorf("CurrentPosition = %d, want 1", state.CurrentPosition)
	}
	if state.SequenceNumber != 1 {
		t.Errorf("SequenceNumber = %d, want 1", state.SequenceNumber)
	}
}

// createTestCluster creates a test cluster with the specified number of nodes.
func createTestCluster(t *testing.T, logger *slog.Logger, nodeCount int) []*Manager {
	t.Helper()

	// Allocate ports
	basePort := 20000
	peers := make([]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		peers[i] = fmt.Sprintf("127.0.0.1:%d", basePort+i)
	}

	managers := make([]*Manager, nodeCount)
	for i := 0; i < nodeCount; i++ {
		config := Config{
			RaftID:            peers[i],
			BindAddr:          peers[i],
			Peers:             peers,
			HeartbeatTimeout:  100 * time.Millisecond,
			ElectionTimeout:   100 * time.Millisecond,
			SnapshotInterval:  1 * time.Hour,
			SnapshotThreshold: 10000,
		}

		manager, err := NewManager(config, logger)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		ctx := context.Background()
		if err := manager.Start(ctx); err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		managers[i] = manager
	}

	return managers
}
