package tools

import (
	"reflect"
	"strings"
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

func TestParseBrowserListEntry(t *testing.T) {
	t.Parallel()

	got := parseBrowserListEntry("Street Kit|loadable=True|folder=False")
	want := BrowserFolderItem{Name: "Street Kit", Loadable: true, Folder: false}
	if got != want {
		t.Errorf("parseBrowserListEntry() = %#v, want %#v", got, want)
	}
}

func TestParseListBrowserFolderResponse(t *testing.T) {
	t.Parallel()

	t.Run("roots", func(t *testing.T) {
		t.Parallel()
		got, err := parseListBrowserFolderResponse([]interface{}{
			"roots",
			"Drums|loadable=False|folder=True",
			"Instruments|loadable=False|folder=True",
		}, "", nil)
		if err != nil {
			t.Fatalf("parseListBrowserFolderResponse() error = %v", err)
		}
		want := ListBrowserFolderOutput{
			Items: []BrowserFolderItem{
				{Name: "Drums", Loadable: false, Folder: true},
				{Name: "Instruments", Loadable: false, Folder: true},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parseListBrowserFolderResponse() = %#v, want %#v", got, want)
		}
	})

	t.Run("folder_children", func(t *testing.T) {
		t.Parallel()
		got, err := parseListBrowserFolderResponse([]interface{}{
			"Drums",
			"Kits",
			"Street Kit|loadable=True|folder=False",
			"Core Kit|loadable=True|folder=False",
		}, "Drums", []string{"Kits"})
		if err != nil {
			t.Fatalf("parseListBrowserFolderResponse() error = %v", err)
		}
		want := ListBrowserFolderOutput{
			RootName:  "Drums",
			PathParts: []string{"Kits"},
			Items: []BrowserFolderItem{
				{Name: "Street Kit", Loadable: true, Folder: false},
				{Name: "Core Kit", Loadable: true, Folder: false},
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parseListBrowserFolderResponse() = %#v, want %#v", got, want)
		}
	})

	t.Run("root_not_found", func(t *testing.T) {
		t.Parallel()
		_, err := parseListBrowserFolderResponse([]interface{}{"root_not_found", "Missing"}, "Missing", nil)
		if err == nil {
			t.Fatal("parseListBrowserFolderResponse() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "root_not_found") {
			t.Errorf("parseListBrowserFolderResponse() error = %q, want root_not_found", err)
		}
	})
}

func TestParseLoadAtPathResponse(t *testing.T) {
	t.Parallel()

	t.Run("loaded", func(t *testing.T) {
		t.Parallel()
		got, err := parseLoadAtPathResponse([]interface{}{int32(3), "loaded", "Street Kit", int32(0), int32(1)})
		if err != nil {
			t.Fatalf("parseLoadAtPathResponse() error = %v", err)
		}
		want := loadAtPathResult{
			TrackIndex:    3,
			Status:        "loaded",
			Loaded:        "Street Kit",
			DevicesBefore: 0,
			DevicesAfter:  1,
		}
		if got != want {
			t.Errorf("parseLoadAtPathResponse() = %#v, want %#v", got, want)
		}
	})

	t.Run("item_not_found", func(t *testing.T) {
		t.Parallel()
		_, err := parseLoadAtPathResponse([]interface{}{int32(0), "item_not_found", "Missing"})
		if err == nil {
			t.Fatal("parseLoadAtPathResponse() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "item_not_found") {
			t.Errorf("parseLoadAtPathResponse() error = %q, want item_not_found", err)
		}
	})
}

func TestLoadAtPathArgs(t *testing.T) {
	t.Parallel()

	got := loadAtPathArgs(2, "Drums", []string{"Kits"}, "Street Kit")
	want := []interface{}{int32(2), int32(-1), "Drums", "Kits", "Street Kit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadAtPathArgs() = %#v, want %#v", got, want)
	}
}
