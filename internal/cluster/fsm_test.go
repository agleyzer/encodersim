package cluster

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	"github.com/hashicorp/raft"
)

func TestPlaylistFSM_Apply_AdvanceWindow_SinglePlaylist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	fsm := NewPlaylistFSM(logger)

	// Initialize state
	initCmd := Command{
		Type: CommandInitialize,
		Data: InitializeCommand{
			State: ClusterState{
				CurrentPosition: 0,
				SequenceNumber:  0,
				TotalSegments:   5,
			},
		},
	}
	initData, err := EncodeCommand(initCmd)
	if err != nil {
		t.Fatalf("failed to encode init command: %v", err)
	}
	fsm.Apply(&raft.Log{Data: initData})

	// Advance window
	advCmd := Command{
		Type: CommandAdvanceWindow,
		Data: AdvanceWindowCommand{VariantIndex: -1},
	}
	advData, err := EncodeCommand(advCmd)
	if err != nil {
		t.Fatalf("failed to encode advance command: %v", err)
	}

	tests := []struct {
		name         string
		wantPosition int
		wantSequence uint64
	}{
		{"first advance", 1, 1},
		{"second advance", 2, 2},
		{"third advance", 3, 3},
		{"fourth advance", 4, 4},
		{"wrap around", 0, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsm.Apply(&raft.Log{Data: advData})
			state := fsm.GetState()

			if state.CurrentPosition != tt.wantPosition {
				t.Errorf("CurrentPosition = %d, want %d", state.CurrentPosition, tt.wantPosition)
			}
			if state.SequenceNumber != tt.wantSequence {
				t.Errorf("SequenceNumber = %d, want %d", state.SequenceNumber, tt.wantSequence)
			}
		})
	}
}

func TestPlaylistFSM_Apply_AdvanceWindow_MultiVariant(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	fsm := NewPlaylistFSM(logger)

	// Initialize with multiple variants
	initCmd := Command{
		Type: CommandInitialize,
		Data: InitializeCommand{
			State: ClusterState{
				Variants: []VariantState{
					{Index: 0, CurrentPosition: 0, SequenceNumber: 0, TotalSegments: 3},
					{Index: 1, CurrentPosition: 0, SequenceNumber: 0, TotalSegments: 5},
				},
			},
		},
	}
	initData, err := EncodeCommand(initCmd)
	if err != nil {
		t.Fatalf("failed to encode init command: %v", err)
	}
	fsm.Apply(&raft.Log{Data: initData})

	// Advance all variants
	advCmd := Command{
		Type: CommandAdvanceWindow,
		Data: AdvanceWindowCommand{VariantIndex: -1},
	}
	advData, err := EncodeCommand(advCmd)
	if err != nil {
		t.Fatalf("failed to encode advance command: %v", err)
	}

	fsm.Apply(&raft.Log{Data: advData})
	state := fsm.GetState()

	if len(state.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(state.Variants))
	}

	if state.Variants[0].CurrentPosition != 1 || state.Variants[0].SequenceNumber != 1 {
		t.Errorf("variant 0: position=%d, sequence=%d, want position=1, sequence=1",
			state.Variants[0].CurrentPosition, state.Variants[0].SequenceNumber)
	}

	if state.Variants[1].CurrentPosition != 1 || state.Variants[1].SequenceNumber != 1 {
		t.Errorf("variant 1: position=%d, sequence=%d, want position=1, sequence=1",
			state.Variants[1].CurrentPosition, state.Variants[1].SequenceNumber)
	}
}

func TestPlaylistFSM_Snapshot_Restore(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	fsm := NewPlaylistFSM(logger)

	// Initialize state
	initCmd := Command{
		Type: CommandInitialize,
		Data: InitializeCommand{
			State: ClusterState{
				CurrentPosition: 3,
				SequenceNumber:  42,
				TotalSegments:   10,
				Variants: []VariantState{
					{Index: 0, CurrentPosition: 5, SequenceNumber: 100, TotalSegments: 8},
				},
			},
		},
	}
	initData, err := EncodeCommand(initCmd)
	if err != nil {
		t.Fatalf("failed to encode init command: %v", err)
	}
	fsm.Apply(&raft.Log{Data: initData})

	// Create snapshot
	snapshot, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	// Persist snapshot
	var buf bytes.Buffer
	sink := &mockSnapshotSink{buf: &buf}
	if err := snapshot.Persist(sink); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}

	// Create new FSM and restore
	fsm2 := NewPlaylistFSM(logger)
	if err := fsm2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Verify state
	state := fsm2.GetState()
	if state.CurrentPosition != 3 {
		t.Errorf("CurrentPosition = %d, want 3", state.CurrentPosition)
	}
	if state.SequenceNumber != 42 {
		t.Errorf("SequenceNumber = %d, want 42", state.SequenceNumber)
	}
	if state.TotalSegments != 10 {
		t.Errorf("TotalSegments = %d, want 10", state.TotalSegments)
	}
	if len(state.Variants) != 1 {
		t.Fatalf("expected 1 variant, got %d", len(state.Variants))
	}
	if state.Variants[0].CurrentPosition != 5 {
		t.Errorf("Variants[0].CurrentPosition = %d, want 5", state.Variants[0].CurrentPosition)
	}
	if state.Variants[0].SequenceNumber != 100 {
		t.Errorf("Variants[0].SequenceNumber = %d, want 100", state.Variants[0].SequenceNumber)
	}
}

func TestPlaylistFSM_GetState_Concurrent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	fsm := NewPlaylistFSM(logger)

	// Initialize state
	initCmd := Command{
		Type: CommandInitialize,
		Data: InitializeCommand{
			State: ClusterState{
				CurrentPosition: 0,
				SequenceNumber:  0,
				TotalSegments:   100,
			},
		},
	}
	initData, err := EncodeCommand(initCmd)
	if err != nil {
		t.Fatalf("failed to encode init command: %v", err)
	}
	fsm.Apply(&raft.Log{Data: initData})

	// Concurrent reads and writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = fsm.GetState()
			}
			done <- true
		}()
	}

	// Advance window concurrently
	advCmd := Command{
		Type: CommandAdvanceWindow,
		Data: AdvanceWindowCommand{VariantIndex: -1},
	}
	advData, err := EncodeCommand(advCmd)
	if err != nil {
		t.Fatalf("failed to encode advance command: %v", err)
	}
	go func() {
		for j := 0; j < 50; j++ {
			fsm.Apply(&raft.Log{Data: advData})
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}

	// Verify final state
	state := fsm.GetState()
	if state.SequenceNumber != 50 {
		t.Errorf("SequenceNumber = %d, want 50", state.SequenceNumber)
	}
}

// mockSnapshotSink implements raft.SnapshotSink for testing.
type mockSnapshotSink struct {
	buf *bytes.Buffer
}

func (m *mockSnapshotSink) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockSnapshotSink) Close() error {
	return nil
}

func (m *mockSnapshotSink) ID() string {
	return "mock"
}

func (m *mockSnapshotSink) Cancel() error {
	return nil
}
