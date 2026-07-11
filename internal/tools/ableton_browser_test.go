package tools

import (
	"testing"
)

func TestParseLoadBrowserItemResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		res        []interface{}
		wantStatus string
		wantItem   string
		wantBefore int
		wantAfter  int
		wantCounts bool
		wantErr    bool
	}{
		{
			name:       "loaded_with_device_counts",
			res:        []interface{}{int32(4), "loaded", "Street Kit", int32(0), int32(1)},
			wantStatus: "loaded",
			wantItem:   "Street Kit",
			wantBefore: 0,
			wantAfter:  1,
			wantCounts: true,
			wantErr:    false,
		},
		{
			name:       "loaded_without_device_counts",
			res:        []interface{}{int32(0), "loaded", "Drum Rack"},
			wantStatus: "loaded",
			wantItem:   "Drum Rack",
			wantCounts: false,
			wantErr:    false,
		},
		{
			name:       "not_found",
			res:        []interface{}{int32(1), "not_found", "Missing Kit"},
			wantStatus: "not_found",
			wantItem:   "Missing Kit",
			wantErr:    true,
		},
		{
			name:    "too_short",
			res:     []interface{}{int32(0), "loaded"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseLoadBrowserItemResponse(tt.res)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLoadBrowserItemResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantStatus == "" {
				return
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.ItemName != tt.wantItem {
				t.Errorf("ItemName = %q, want %q", got.ItemName, tt.wantItem)
			}
			if tt.wantCounts {
				if got.DevicesBefore == nil || *got.DevicesBefore != tt.wantBefore {
					t.Errorf("DevicesBefore = %v, want %d", got.DevicesBefore, tt.wantBefore)
				}
				if got.DevicesAfter == nil || *got.DevicesAfter != tt.wantAfter {
					t.Errorf("DevicesAfter = %v, want %d", got.DevicesAfter, tt.wantAfter)
				}
			}
		})
	}
}

func TestParseLoadDevicePresetResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		res        []interface{}
		wantStatus string
		wantPreset string
		wantErr    bool
	}{
		{
			name:       "loaded",
			res:        []interface{}{int32(2), int32(0), "loaded", "Glocken Pluck"},
			wantStatus: "loaded",
			wantPreset: "Glocken Pluck",
			wantErr:    false,
		},
		{
			name:       "not_found",
			res:        []interface{}{int32(2), int32(0), "not_found", "Missing"},
			wantStatus: "not_found",
			wantPreset: "Missing",
			wantErr:    true,
		},
		{
			name:       "invalid_device_index",
			res:        []interface{}{int32(2), int32(9), "invalid_device_index", "Any"},
			wantStatus: "invalid_device_index",
			wantPreset: "Any",
			wantErr:    true,
		},
		{
			name:    "too_short",
			res:     []interface{}{int32(0), int32(0), "loaded"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseLoadDevicePresetResponse(tt.res)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLoadDevicePresetResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantStatus == "" {
				return
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.PresetName != tt.wantPreset {
				t.Errorf("PresetName = %q, want %q", got.PresetName, tt.wantPreset)
			}
		})
	}
}
