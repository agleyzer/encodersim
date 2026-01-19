package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ClusterTestHarness manages a multi-instance cluster for integration tests.
type ClusterTestHarness struct {
	t           *testing.T
	httpServer  *http.Server
	httpPort    int
	instances   []*ClusterInstance
	tempDir     string
	playlistURL string
}

// ClusterInstance represents a single encodersim instance in the cluster.
type ClusterInstance struct {
	ID       string
	HTTPPort int
	RaftPort int
	Cmd      *exec.Cmd
	Cancel   context.CancelFunc
}

// NewClusterTestHarness creates a new cluster test harness.
func NewClusterTestHarness(t *testing.T, nodeCount int) *ClusterTestHarness {
	t.Helper()

	if nodeCount < 1 {
		t.Fatal("nodeCount must be at least 1")
	}

	// Find available port for HTTP server
	httpPort := findAvailablePort(t)

	return &ClusterTestHarness{
		t:         t,
		httpPort:  httpPort,
		instances: make([]*ClusterInstance, 0, nodeCount),
	}
}

// StartHTTPServer starts an HTTP server serving test playlists.
func (h *ClusterTestHarness) StartHTTPServer(playlistContent string, playlistName string) {
	h.t.Helper()

	// Create a temporary directory for test files
	h.tempDir = h.t.TempDir()

	// Write playlist content to file
	playlistPath := h.tempDir + "/" + playlistName
	if err := os.WriteFile(playlistPath, []byte(playlistContent), 0644); err != nil {
		h.t.Fatalf("failed to write test playlist: %v", err)
	}

	// Create file server
	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir(h.tempDir))
	mux.Handle("/", fileServer)

	h.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", h.httpPort),
		Handler: mux,
	}

	// Start server in goroutine
	go func() {
		if err := h.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.t.Logf("HTTP server error: %v", err)
		}
	}()

	// Wait for server to be ready
	waitForServer(h.t, fmt.Sprintf("http://localhost:%d", h.httpPort), 5*time.Second)

	h.playlistURL = fmt.Sprintf("http://localhost:%d/%s", h.httpPort, playlistName)
	h.t.Logf("HTTP server started on port %d, playlist URL: %s", h.httpPort, h.playlistURL)
}

// StartCluster starts a cluster with the specified number of nodes.
func (h *ClusterTestHarness) StartCluster(nodeCount int) error {
	h.t.Helper()

	if h.playlistURL == "" {
		h.t.Fatal("StartHTTPServer must be called before StartCluster")
	}

	// Allocate ports for all nodes
	httpPorts := make([]int, nodeCount)
	raftPorts := make([]int, nodeCount)
	peerAddrs := make([]string, nodeCount)

	for i := 0; i < nodeCount; i++ {
		httpPorts[i] = findAvailablePort(h.t)
		raftPorts[i] = findAvailablePort(h.t)
		peerAddrs[i] = fmt.Sprintf("127.0.0.1:%d", raftPorts[i])
	}

	peersStr := strings.Join(peerAddrs, ",")

	// Start each node
	for i := 0; i < nodeCount; i++ {
		nodeID := fmt.Sprintf("node%d", i+1)

		ctx, cancel := context.WithCancel(context.Background())

		cmd := exec.CommandContext(ctx, "./encodersim",
			"--cluster",
			"--raft-id", nodeID,
			"--raft-bind", peerAddrs[i],
			"--peers", peersStr,
			"--port", strconv.Itoa(httpPorts[i]),
			"--window-size", "3",
			h.playlistURL,
		)

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			cancel()
			return fmt.Errorf("failed to start node %s: %w", nodeID, err)
		}

		instance := &ClusterInstance{
			ID:       nodeID,
			HTTPPort: httpPorts[i],
			RaftPort: raftPorts[i],
			Cmd:      cmd,
			Cancel:   cancel,
		}

		h.instances = append(h.instances, instance)
		h.t.Logf("Started node %s (HTTP: %d, Raft: %d)", nodeID, httpPorts[i], raftPorts[i])
	}

	// Wait for all instances to be ready
	for _, inst := range h.instances {
		url := fmt.Sprintf("http://localhost:%d/health", inst.HTTPPort)
		waitForServer(h.t, url, 15*time.Second)
	}

	// Wait for leader election
	if err := h.WaitForLeader(10 * time.Second); err != nil {
		return fmt.Errorf("leader election failed: %w", err)
	}

	h.t.Logf("Cluster started with %d nodes", nodeCount)
	return nil
}

// WaitForLeader waits until a leader is elected.
func (h *ClusterTestHarness) WaitForLeader(timeout time.Duration) error {
	h.t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C

		for _, inst := range h.instances {
			status, err := h.GetClusterStatus(inst)
			if err != nil {
				continue
			}

			if isLeader, ok := status["is_leader"].(bool); ok && isLeader {
				h.t.Logf("Leader elected: %s", inst.ID)
				return nil
			}
		}
	}

	return fmt.Errorf("leader election timeout after %v", timeout)
}

// GetLeader returns the leader instance.
func (h *ClusterTestHarness) GetLeader() (*ClusterInstance, error) {
	h.t.Helper()

	for _, inst := range h.instances {
		status, err := h.GetClusterStatus(inst)
		if err != nil {
			continue
		}

		if isLeader, ok := status["is_leader"].(bool); ok && isLeader {
			return inst, nil
		}
	}

	return nil, fmt.Errorf("no leader found")
}

// GetClusterStatus fetches cluster status from an instance.
func (h *ClusterTestHarness) GetClusterStatus(inst *ClusterInstance) (map[string]any, error) {
	h.t.Helper()

	url := fmt.Sprintf("http://localhost:%d/cluster/status", inst.HTTPPort)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}

	return status, nil
}

// FetchPlaylist fetches the playlist from an instance.
func (h *ClusterTestHarness) FetchPlaylist(inst *ClusterInstance) (string, error) {
	h.t.Helper()

	url := fmt.Sprintf("http://localhost:%d/playlist.m3u8", inst.HTTPPort)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// VerifyPlaylistConsistency verifies all instances serve identical playlists.
func (h *ClusterTestHarness) VerifyPlaylistConsistency() error {
	h.t.Helper()

	if len(h.instances) == 0 {
		return fmt.Errorf("no instances running")
	}

	// Fetch playlist from first instance
	firstPlaylist, err := h.FetchPlaylist(h.instances[0])
	if err != nil {
		return fmt.Errorf("failed to fetch from %s: %w", h.instances[0].ID, err)
	}

	// Compare with all other instances
	for i := 1; i < len(h.instances); i++ {
		playlist, err := h.FetchPlaylist(h.instances[i])
		if err != nil {
			return fmt.Errorf("failed to fetch from %s: %w", h.instances[i].ID, err)
		}

		if playlist != firstPlaylist {
			return fmt.Errorf("playlist mismatch between %s and %s", h.instances[0].ID, h.instances[i].ID)
		}
	}

	return nil
}

// StopInstance stops a specific instance.
func (h *ClusterTestHarness) StopInstance(inst *ClusterInstance) error {
	h.t.Helper()

	inst.Cancel()
	if err := inst.Cmd.Wait(); err != nil {
		// Ignore error if process was killed
		if !strings.Contains(err.Error(), "signal: killed") {
			return err
		}
	}

	h.t.Logf("Stopped node %s", inst.ID)
	return nil
}

// Cleanup stops all instances and cleans up resources.
func (h *ClusterTestHarness) Cleanup() {
	h.t.Helper()

	// Stop all instances
	for _, inst := range h.instances {
		inst.Cancel()
		_ = inst.Cmd.Wait() // Ignore errors during cleanup
	}

	// Stop HTTP server
	if h.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.httpServer.Shutdown(ctx)
	}
}

// waitForServer waits for a server to become available.
func waitForServer(t *testing.T, url string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C

		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
	}

	t.Fatalf("server at %s did not become ready within %v", url, timeout)
}

// TestThreeNodeCluster tests a basic 3-node cluster.
func TestThreeNodeCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cluster integration test in short mode")
	}

	harness := NewClusterTestHarness(t, 3)
	defer harness.Cleanup()

	// Create test playlist
	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
http://example.com/segment0.ts
#EXTINF:10.0,
http://example.com/segment1.ts
#EXTINF:10.0,
http://example.com/segment2.ts
#EXTINF:10.0,
http://example.com/segment3.ts
#EXTINF:10.0,
http://example.com/segment4.ts
#EXT-X-ENDLIST`

	harness.StartHTTPServer(playlist, "playlist.m3u8")

	if err := harness.StartCluster(3); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}

	// Verify leader is elected
	leader, err := harness.GetLeader()
	if err != nil {
		t.Fatalf("failed to get leader: %v", err)
	}
	t.Logf("Leader is %s", leader.ID)

	// Verify all instances serve identical playlists
	if err := harness.VerifyPlaylistConsistency(); err != nil {
		t.Fatalf("playlist consistency check failed: %v", err)
	}

	// Wait for window to advance
	time.Sleep(15 * time.Second)

	// Verify playlists are still consistent after advancement
	if err := harness.VerifyPlaylistConsistency(); err != nil {
		t.Fatalf("playlist consistency check failed after advancement: %v", err)
	}

	t.Log("Cluster test passed")
}

// TestLeaderElection tests leader election after leader failure.
func TestLeaderElection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cluster integration test in short mode")
	}

	harness := NewClusterTestHarness(t, 3)
	defer harness.Cleanup()

	// Create test playlist
	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:10.0,
http://example.com/segment0.ts
#EXTINF:10.0,
http://example.com/segment1.ts
#EXTINF:10.0,
http://example.com/segment2.ts
#EXTINF:10.0,
http://example.com/segment3.ts
#EXTINF:10.0,
http://example.com/segment4.ts
#EXT-X-ENDLIST`

	harness.StartHTTPServer(playlist, "playlist.m3u8")

	if err := harness.StartCluster(3); err != nil {
		t.Fatalf("failed to start cluster: %v", err)
	}

	// Get initial leader
	leader, err := harness.GetLeader()
	if err != nil {
		t.Fatalf("failed to get leader: %v", err)
	}
	t.Logf("Initial leader is %s", leader.ID)

	// Stop the leader
	t.Logf("Stopping leader %s", leader.ID)
	if err := harness.StopInstance(leader); err != nil {
		t.Fatalf("failed to stop leader: %v", err)
	}

	// Wait for new leader election
	time.Sleep(5 * time.Second)
	if err := harness.WaitForLeader(10 * time.Second); err != nil {
		t.Fatalf("new leader election failed: %v", err)
	}

	// Get new leader
	newLeader, err := harness.GetLeader()
	if err != nil {
		t.Fatalf("failed to get new leader: %v", err)
	}
	t.Logf("New leader is %s", newLeader.ID)

	if newLeader.ID == leader.ID {
		t.Fatal("new leader is the same as old leader")
	}

	// Verify remaining instances still serve playlists
	remainingInstances := make([]*ClusterInstance, 0, len(harness.instances)-1)
	for _, inst := range harness.instances {
		if inst.ID != leader.ID {
			remainingInstances = append(remainingInstances, inst)
		}
	}

	// Check consistency among remaining instances
	if len(remainingInstances) > 1 {
		firstPlaylist, err := harness.FetchPlaylist(remainingInstances[0])
		if err != nil {
			t.Fatalf("failed to fetch playlist after leader failure: %v", err)
		}

		secondPlaylist, err := harness.FetchPlaylist(remainingInstances[1])
		if err != nil {
			t.Fatalf("failed to fetch playlist after leader failure: %v", err)
		}

		if firstPlaylist != secondPlaylist {
			t.Fatal("playlist inconsistency after leader election")
		}
	}

	t.Log("Leader election test passed")
}
