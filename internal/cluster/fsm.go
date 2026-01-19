// Package cluster provides Raft-based distributed state management for encodersim.
package cluster

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/hashicorp/raft"
)

func init() {
	// Register types for gob encoding/decoding
	gob.Register(AdvanceWindowCommand{})
	gob.Register(InitializeCommand{})
}

// ClusterState represents the shared state across all cluster nodes.
type ClusterState struct {
	// CurrentPosition is the sliding window start index (for single media playlists).
	CurrentPosition int
	// SequenceNumber is the HLS media sequence number (for single media playlists).
	SequenceNumber uint64
	// Variants holds per-variant state (for multi-variant master playlists).
	Variants []VariantState
	// TotalSegments is the total number of segments in the playlist.
	TotalSegments int
}

// VariantState represents state for a single variant in a multi-variant playlist.
type VariantState struct {
	// Index is the variant index.
	Index int
	// CurrentPosition is the sliding window start index for this variant.
	CurrentPosition int
	// SequenceNumber is the HLS media sequence number for this variant.
	SequenceNumber uint64
	// TotalSegments is the total number of segments for this variant.
	TotalSegments int
}

// CommandType identifies the type of Raft command.
type CommandType uint8

const (
	// CommandAdvanceWindow advances the sliding window.
	CommandAdvanceWindow CommandType = 1
	// CommandInitialize initializes the FSM state.
	CommandInitialize CommandType = 2
)

// Command represents a Raft log command.
type Command struct {
	Type CommandType
	Data any
}

// AdvanceWindowCommand advances the window for all variants.
type AdvanceWindowCommand struct {
	// VariantIndex specifies which variant to advance (-1 for all variants).
	VariantIndex int
}

// InitializeCommand sets the initial state.
type InitializeCommand struct {
	State ClusterState
}

// PlaylistFSM implements the raft.FSM interface for playlist state management.
type PlaylistFSM struct {
	mu     sync.RWMutex
	state  ClusterState
	logger *slog.Logger
}

// NewPlaylistFSM creates a new PlaylistFSM.
func NewPlaylistFSM(logger *slog.Logger) *PlaylistFSM {
	return &PlaylistFSM{
		state: ClusterState{
			CurrentPosition: 0,
			SequenceNumber:  0,
			Variants:        []VariantState{},
		},
		logger: logger,
	}
}

// Apply applies a Raft log entry to the FSM.
func (f *PlaylistFSM) Apply(log *raft.Log) any {
	var cmd Command
	if err := gob.NewDecoder(bytes.NewReader(log.Data)).Decode(&cmd); err != nil {
		f.logger.Error("failed to decode command", "error", err)
		return fmt.Errorf("decode command: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	switch cmd.Type {
	case CommandAdvanceWindow:
		return f.applyAdvanceWindow(cmd.Data)
	case CommandInitialize:
		return f.applyInitialize(cmd.Data)
	default:
		f.logger.Error("unknown command type", "type", cmd.Type)
		return fmt.Errorf("unknown command type: %d", cmd.Type)
	}
}

// applyAdvanceWindow advances the window position and sequence number.
func (f *PlaylistFSM) applyAdvanceWindow(data any) any {
	advCmd, ok := data.(AdvanceWindowCommand)
	if !ok {
		return fmt.Errorf("invalid advance window command data")
	}

	if len(f.state.Variants) == 0 {
		// Single media playlist mode
		if f.state.TotalSegments > 0 {
			f.state.CurrentPosition = (f.state.CurrentPosition + 1) % f.state.TotalSegments
		}
		f.state.SequenceNumber++
		f.logger.Debug("advanced window", "position", f.state.CurrentPosition, "sequence", f.state.SequenceNumber)
	} else {
		// Multi-variant mode
		if advCmd.VariantIndex == -1 {
			// Advance all variants
			for i := range f.state.Variants {
				if f.state.Variants[i].TotalSegments > 0 {
					f.state.Variants[i].CurrentPosition = (f.state.Variants[i].CurrentPosition + 1) % f.state.Variants[i].TotalSegments
				}
				f.state.Variants[i].SequenceNumber++
			}
			f.logger.Debug("advanced all variants")
		} else {
			// Advance specific variant
			if advCmd.VariantIndex >= 0 && advCmd.VariantIndex < len(f.state.Variants) {
				v := &f.state.Variants[advCmd.VariantIndex]
				if v.TotalSegments > 0 {
					v.CurrentPosition = (v.CurrentPosition + 1) % v.TotalSegments
				}
				v.SequenceNumber++
				f.logger.Debug("advanced variant", "index", advCmd.VariantIndex, "position", v.CurrentPosition, "sequence", v.SequenceNumber)
			}
		}
	}

	return nil
}

// applyInitialize sets the initial FSM state.
func (f *PlaylistFSM) applyInitialize(data any) any {
	initCmd, ok := data.(InitializeCommand)
	if !ok {
		return fmt.Errorf("invalid initialize command data")
	}

	f.state = initCmd.State
	f.logger.Info("initialized FSM state", "variants", len(f.state.Variants), "total_segments", f.state.TotalSegments)
	return nil
}

// Snapshot returns an FSMSnapshot for creating a point-in-time snapshot.
func (f *PlaylistFSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Deep copy the state
	stateCopy := ClusterState{
		CurrentPosition: f.state.CurrentPosition,
		SequenceNumber:  f.state.SequenceNumber,
		TotalSegments:   f.state.TotalSegments,
		Variants:        make([]VariantState, len(f.state.Variants)),
	}
	copy(stateCopy.Variants, f.state.Variants)

	return &fsmSnapshot{state: stateCopy}, nil
}

// Restore restores the FSM state from a snapshot.
func (f *PlaylistFSM) Restore(snapshot io.ReadCloser) error {
	defer snapshot.Close()

	var state ClusterState
	if err := gob.NewDecoder(snapshot).Decode(&state); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	f.mu.Lock()
	f.state = state
	f.mu.Unlock()

	f.logger.Info("restored FSM state from snapshot", "variants", len(state.Variants), "total_segments", state.TotalSegments)
	return nil
}

// GetState returns a copy of the current FSM state.
func (f *PlaylistFSM) GetState() ClusterState {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Deep copy the state
	stateCopy := ClusterState{
		CurrentPosition: f.state.CurrentPosition,
		SequenceNumber:  f.state.SequenceNumber,
		TotalSegments:   f.state.TotalSegments,
		Variants:        make([]VariantState, len(f.state.Variants)),
	}
	copy(stateCopy.Variants, f.state.Variants)

	return stateCopy
}

// fsmSnapshot implements raft.FSMSnapshot.
type fsmSnapshot struct {
	state ClusterState
}

// Persist writes the snapshot to the given sink.
func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(s.state); err != nil {
		sink.Cancel()
		return fmt.Errorf("encode snapshot: %w", err)
	}

	if _, err := sink.Write(buf.Bytes()); err != nil {
		sink.Cancel()
		return fmt.Errorf("write snapshot: %w", err)
	}

	return sink.Close()
}

// Release releases any resources held by the snapshot.
func (s *fsmSnapshot) Release() {}

// EncodeCommand encodes a command for Raft submission.
func EncodeCommand(cmd Command) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}
	return buf.Bytes(), nil
}
