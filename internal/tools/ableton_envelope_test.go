package tools

import (
	"strings"
	"testing"
)

func TestParseEnvelopeGetOK(t *testing.T) {
	res := []interface{}{
		int32(0), int32(1), "ok", "Volume",
		float32(0), float32(0.5),
		float32(1), float32(0.8),
	}
	out, err := parseEnvelopeGet(res, -1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Exists || out.ParamName != "Volume" || len(out.Samples) != 2 {
		t.Fatalf("out = %+v", out)
	}
	if out.Samples[1].Time != 1 || out.Samples[1].Value < 0.79 || out.Samples[1].Value > 0.81 {
		t.Errorf("sample1 = %+v", out.Samples[1])
	}
}

func TestParseEnvelopeGetMissing(t *testing.T) {
	out, err := parseEnvelopeGet([]interface{}{int32(0), int32(0), "missing", "Volume"}, -1, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Exists {
		t.Error("expected exists=false")
	}
	if !strings.Contains(out.Hint, "set_clip_envelope") {
		t.Errorf("hint = %q", out.Hint)
	}
}

func TestParseEnvelopeGetNoClip(t *testing.T) {
	_, err := parseEnvelopeGet([]interface{}{int32(0), int32(0), "no_clip"}, -1, 0)
	if err == nil || !strings.Contains(err.Error(), "no_clip") {
		t.Fatalf("got %v", err)
	}
}

func TestParseEnvelopeSetStepsOK(t *testing.T) {
	out, err := parseEnvelopeSetSteps([]interface{}{int32(0), int32(0), "ok", int32(3), "Volume"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.StepsSet != 3 || out.ParamName != "Volume" {
		t.Fatalf("out = %+v", out)
	}
}

func TestParseEnvelopeSetStepsCreateError(t *testing.T) {
	_, err := parseEnvelopeSetSteps([]interface{}{int32(0), int32(0), "error", "create", "boom"})
	if err == nil || !strings.Contains(err.Error(), "envelope_write_failed") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateEnvelopeTarget(t *testing.T) {
	if err := validateEnvelopeTarget(ClipEnvelopeTarget{TrackIndex: 0, ClipIndex: 0, DeviceIndex: -1, ParameterIndex: 0}); err != nil {
		t.Fatalf("valid mixer target failed: %v", err)
	}
	if err := validateEnvelopeTarget(ClipEnvelopeTarget{TrackIndex: 0, ClipIndex: 0, DeviceIndex: -2, ParameterIndex: 0}); err == nil {
		t.Fatal("expected error for device_index < -1")
	}
}
