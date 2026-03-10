package tools

import (
	"testing"
)

func TestValidateTrackClipIndices(t *testing.T) {
	tests := []struct {
		name       string
		trackIndex int
		clipIndex  int
		wantErr    bool
	}{
		{"both_valid", 0, 0, false},
		{"large_indices", 10, 20, false},
		{"negative_track", -1, 0, true},
		{"negative_clip", 0, -1, true},
		{"both_negative", -1, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrackClipIndices(tt.trackIndex, tt.clipIndex)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTrackClipIndices(%d, %d) error = %v, wantErr %v", tt.trackIndex, tt.clipIndex, err, tt.wantErr)
			}
		})
	}
}

func TestEnsureResponseLen(t *testing.T) {
	tests := []struct {
		name    string
		res     []interface{}
		min     int
		wantErr bool
	}{
		{"exact", []interface{}{1, 2}, 2, false},
		{"more_than_min", []interface{}{1, 2, 3}, 2, false},
		{"less_than_min", []interface{}{1}, 2, true},
		{"empty", []interface{}{}, 1, true},
		{"zero_min", []interface{}{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureResponseLen(tt.res, tt.min)
			if (err != nil) != tt.wantErr {
				t.Errorf("ensureResponseLen(%v, %d) error = %v, wantErr %v", tt.res, tt.min, err, tt.wantErr)
			}
		})
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []interface{}
		want  []string
	}{
		{"strings", []interface{}{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"ints", []interface{}{1, 2, 3}, []string{"1", "2", "3"}},
		{"mixed", []interface{}{"hello", 42, 3.14, true}, []string{"hello", "42", "3.14", "true"}},
		{"empty", []interface{}{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toStringSlice(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("toStringSlice(%v) len = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("toStringSlice(%v)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
