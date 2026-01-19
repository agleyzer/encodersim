package main

import (
	"testing"
	"time"

	"github.com/agleyzer/encodersim/internal/segment"
)

func TestCalculateSegmentSubset(t *testing.T) {
	tests := []struct {
		name        string
		segments    []segment.Segment
		maxDuration time.Duration
		wantCount   int
		wantTotal   float64 // Expected total duration in seconds
	}{
		{
			name: "zero duration returns all segments",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 10.0},
				{URL: "seg1.ts", Duration: 10.0},
				{URL: "seg2.ts", Duration: 10.0},
			},
			maxDuration: 0,
			wantCount:   3,
			wantTotal:   30.0,
		},
		{
			name:        "empty segments returns empty",
			segments:    []segment.Segment{},
			maxDuration: 10 * time.Second,
			wantCount:   0,
			wantTotal:   0.0,
		},
		{
			name: "first segment longer than duration returns first segment",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 15.0},
				{URL: "seg1.ts", Duration: 10.0},
			},
			maxDuration: 10 * time.Second,
			wantCount:   1,
			wantTotal:   15.0,
		},
		{
			name: "exact fit includes segments up to 50% threshold",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 5.0},
				{URL: "seg1.ts", Duration: 5.0},
				{URL: "seg2.ts", Duration: 5.0}, // Total 15s, exceeds 10s by exactly 50%
			},
			maxDuration: 10 * time.Second,
			wantCount:   3,
			wantTotal:   15.0,
		},
		{
			name: "includes segment within 50% threshold",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 10.0},
				{URL: "seg1.ts", Duration: 4.0}, // Total 14s, exceeds 10s by 40%
			},
			maxDuration: 10 * time.Second,
			wantCount:   2,
			wantTotal:   14.0,
		},
		{
			name: "excludes segment exceeding 50% threshold",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 10.0},
				{URL: "seg1.ts", Duration: 6.0}, // Total 16s, exceeds 10s by 60%
			},
			maxDuration: 10 * time.Second,
			wantCount:   1,
			wantTotal:   10.0,
		},
		{
			name: "multiple segments within threshold",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 2.0},
				{URL: "seg1.ts", Duration: 2.0},
				{URL: "seg2.ts", Duration: 2.0},
				{URL: "seg3.ts", Duration: 2.0},
				{URL: "seg4.ts", Duration: 2.0},
				{URL: "seg5.ts", Duration: 2.0}, // Total 12s, exceeds 10s by 20%
			},
			maxDuration: 10 * time.Second,
			wantCount:   6,
			wantTotal:   12.0,
		},
		{
			name: "real-world case with 30 second limit",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 9.9},
				{URL: "seg1.ts", Duration: 10.0},
				{URL: "seg2.ts", Duration: 10.1},
				{URL: "seg3.ts", Duration: 10.0}, // Total 40s, exceeds 30s by 33%
				{URL: "seg4.ts", Duration: 10.0},
			},
			maxDuration: 30 * time.Second,
			wantCount:   4,
			wantTotal:   40.0,
		},
		{
			name: "boundary case at exactly 50% threshold",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 10.0},
				{URL: "seg1.ts", Duration: 5.0}, // Total 15s, exceeds 10s by exactly 50%
			},
			maxDuration: 10 * time.Second,
			wantCount:   2,
			wantTotal:   15.0,
		},
		{
			name: "very short duration with longer segments",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 10.0},
				{URL: "seg1.ts", Duration: 10.0},
			},
			maxDuration: 1 * time.Second,
			wantCount:   1,
			wantTotal:   10.0,
		},
		{
			name: "stops when next segment would exceed by more than 50%",
			segments: []segment.Segment{
				{URL: "seg0.ts", Duration: 8.0},
				{URL: "seg1.ts", Duration: 8.0}, // Total 16s, exceeds 10s by 60%
				{URL: "seg2.ts", Duration: 8.0},
			},
			maxDuration: 10 * time.Second,
			wantCount:   1,
			wantTotal:   8.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateSegmentSubset(tt.segments, tt.maxDuration)

			if len(result) != tt.wantCount {
				t.Errorf("calculateSegmentSubset() returned %d segments, want %d",
					len(result), tt.wantCount)
			}

			// Calculate total duration
			var totalDuration float64
			for _, seg := range result {
				totalDuration += seg.Duration
			}

			if totalDuration != tt.wantTotal {
				t.Errorf("calculateSegmentSubset() total duration = %.1f, want %.1f",
					totalDuration, tt.wantTotal)
			}

			// Verify segments are in order
			for i, seg := range result {
				if seg.URL != tt.segments[i].URL {
					t.Errorf("segment[%d] URL = %s, want %s",
						i, seg.URL, tt.segments[i].URL)
				}
			}
		})
	}
}

func TestCalculateSegmentSubset_PreservesSegmentFields(t *testing.T) {
	segments := []segment.Segment{
		{URL: "seg0.ts", Duration: 5.0, Sequence: 100, VariantIndex: 2},
		{URL: "seg1.ts", Duration: 5.0, Sequence: 101, VariantIndex: 2},
	}

	result := calculateSegmentSubset(segments, 10*time.Second)

	if len(result) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result))
	}

	// Verify all fields are preserved
	for i, seg := range result {
		if seg.URL != segments[i].URL {
			t.Errorf("segment[%d] URL not preserved", i)
		}
		if seg.Duration != segments[i].Duration {
			t.Errorf("segment[%d] Duration not preserved", i)
		}
		if seg.Sequence != segments[i].Sequence {
			t.Errorf("segment[%d] Sequence not preserved", i)
		}
		if seg.VariantIndex != segments[i].VariantIndex {
			t.Errorf("segment[%d] VariantIndex not preserved", i)
		}
	}
}
