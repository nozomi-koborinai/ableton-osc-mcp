package tools

import (
	"strings"
	"testing"
)

func TestBuildDeviceParametersMergesDisplayAndQuantized(t *testing.T) {
	names := []string{"1 Frequency A", "1 Filter Type A"}
	values := []interface{}{float32(0.17), int32(1)}
	mins := []interface{}{float32(0), float32(0)}
	maxs := []interface{}{float32(1), int32(7)}
	displays := []interface{}{"37.0 Hz", "Low Shelf"}
	quantized := []interface{}{false, true}

	params, err := buildDeviceParameters(names, values, mins, maxs, displays, quantized)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(params))
	}

	if params[0].Name != "1 Frequency A" || params[0].DisplayValue != "37.0 Hz" {
		t.Errorf("param0 = %+v, want name/display '1 Frequency A' / '37.0 Hz'", params[0])
	}
	if params[0].IsQuantized == nil || *params[0].IsQuantized {
		t.Errorf("param0 IsQuantized = %v, want non-nil false", params[0].IsQuantized)
	}
	if params[1].DisplayValue != "Low Shelf" {
		t.Errorf("param1 display = %q, want 'Low Shelf'", params[1].DisplayValue)
	}
	if params[1].IsQuantized == nil || !*params[1].IsQuantized {
		t.Errorf("param1 IsQuantized = %v, want non-nil true", params[1].IsQuantized)
	}
}

func TestBuildDeviceParametersBestEffortOptionalFields(t *testing.T) {
	names := []string{"Volume", "Filter Freq"}
	values := []interface{}{float32(0.85), float32(0.5)}
	mins := []interface{}{float32(0), float32(0)}
	maxs := []interface{}{float32(1), float32(1)}

	// Patch missing: no display strings and no quantized flags at all.
	params, err := buildDeviceParameters(names, values, mins, maxs, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, p := range params {
		if p.DisplayValue != "" {
			t.Errorf("param%d display = %q, want empty when patch missing", i, p.DisplayValue)
		}
		if p.IsQuantized != nil {
			t.Errorf("param%d IsQuantized = %v, want nil when unavailable", i, p.IsQuantized)
		}
	}

	// Shorter displays slice: only the first parameter gets a display value.
	params, err = buildDeviceParameters(names, values, mins, maxs, []interface{}{"-6.0 dB"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params[0].DisplayValue != "-6.0 dB" {
		t.Errorf("param0 display = %q, want '-6.0 dB'", params[0].DisplayValue)
	}
	if params[1].DisplayValue != "" {
		t.Errorf("param1 display = %q, want empty (displays too short)", params[1].DisplayValue)
	}
}

func TestBuildDeviceParametersLengthMismatch(t *testing.T) {
	names := []string{"A", "B"}
	values := []interface{}{float32(1)}
	mins := []interface{}{float32(0), float32(0)}
	maxs := []interface{}{float32(1), float32(1)}

	if _, err := buildDeviceParameters(names, values, mins, maxs, nil, nil); err == nil {
		t.Fatal("expected error on length mismatch, got nil")
	}
}

func TestParseSetDeviceParameterStringResponseSet(t *testing.T) {
	res := []interface{}{int32(3), int32(1), int32(12), "set", float32(1), "Ins"}
	out, err := parseSetDeviceParameterStringResponse(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "set" {
		t.Errorf("status = %q, want set", out.Status)
	}
	if out.Value != 1 {
		t.Errorf("value = %v, want 1", out.Value)
	}
	if out.DisplayValue != "Ins" {
		t.Errorf("display = %q, want Ins", out.DisplayValue)
	}
}

func TestParseSetDeviceParameterStringResponseNoMatchWithOptions(t *testing.T) {
	res := []interface{}{int32(3), int32(1), int32(12), "no_match", "Xyz", "Mix", "Ins", "Gate"}
	out, err := parseSetDeviceParameterStringResponse(res)
	if err == nil {
		t.Fatal("expected error for no_match, got nil")
	}
	if len(out.Options) != 3 || out.Options[1] != "Ins" {
		t.Errorf("options = %v, want [Mix Ins Gate]", out.Options)
	}
}

func TestParseSetDeviceParameterStringResponseInvalid(t *testing.T) {
	res := []interface{}{int32(3), int32(1), int32(99), "invalid_parameter_index"}
	out, err := parseSetDeviceParameterStringResponse(res)
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if out.Status != "invalid_parameter_index" {
		t.Errorf("status = %q, want invalid_parameter_index", out.Status)
	}
}

func TestParseDeleteDeviceResponseDeleted(t *testing.T) {
	res := []interface{}{int32(2), int32(1), "deleted", "EQ Eight", int32(3), int32(2)}
	out, err := parseDeleteDeviceResponse(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "deleted" {
		t.Errorf("status = %q, want deleted", out.Status)
	}
	if out.DeviceName != "EQ Eight" {
		t.Errorf("device_name = %q, want 'EQ Eight'", out.DeviceName)
	}
	if out.DevicesBefore == nil || *out.DevicesBefore != 3 {
		t.Errorf("devices_before = %v, want 3", out.DevicesBefore)
	}
	if out.DevicesAfter == nil || *out.DevicesAfter != 2 {
		t.Errorf("devices_after = %v, want 2", out.DevicesAfter)
	}
}

func TestParseDeleteDeviceResponseInvalid(t *testing.T) {
	res := []interface{}{int32(2), int32(9), "invalid_device_index"}
	out, err := parseDeleteDeviceResponse(res)
	if err == nil {
		t.Fatal("expected error for invalid_device_index, got nil")
	}
	if out.Status != "invalid_device_index" {
		t.Errorf("status = %q, want invalid_device_index", out.Status)
	}
	if out.DevicesAfter != nil {
		t.Errorf("devices_after = %v, want nil on failure", out.DevicesAfter)
	}
}

func TestParseDeleteDeviceResponseError(t *testing.T) {
	res := []interface{}{int32(2), int32(0), "error", "boom"}
	_, err := parseDeleteDeviceResponse(res)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want to contain detail 'boom'", err)
	}
}
