package tools

import (
	"strings"
	"testing"
)

func TestParseReturnTracks(t *testing.T) {
	res := []interface{}{int32(2), "A", "B"}
	out, err := parseReturnTracks(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Returns) != 2 || out.Returns[0].Name != "A" || out.Returns[1].Index != 1 {
		t.Fatalf("out = %+v", out)
	}
}

func TestParseReturnTracksEmpty(t *testing.T) {
	out, err := parseReturnTracks([]interface{}{int32(0)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Returns == nil || len(out.Returns) != 0 {
		t.Fatalf("want empty list, got %+v", out.Returns)
	}
}

func TestParseDeviceRoutingListOK(t *testing.T) {
	res := []interface{}{int32(1), int32(0), "ok", "Drums", "Kick"}
	track, device, names, err := parseDeviceRoutingList(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if track != 1 || device != 0 || len(names) != 2 || names[0] != "Drums" {
		t.Fatalf("got %d %d %v", track, device, names)
	}
}

func TestParseDeviceRoutingListUnsupported(t *testing.T) {
	res := []interface{}{int32(0), int32(0), "unsupported"}
	_, _, _, err := parseDeviceRoutingList(res)
	if err == nil || !strings.Contains(err.Error(), "unsupported_device") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "next:") {
		t.Errorf("missing next step: %v", err)
	}
}

func TestParseDeviceRoutingSetNotFound(t *testing.T) {
	res := []interface{}{int32(0), int32(0), "not_found", "Ghost", "Drums", "Bass"}
	_, err := parseDeviceRoutingSet(res)
	if err == nil || !strings.Contains(err.Error(), "routing_not_found") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "Drums") {
		t.Errorf("options missing from error: %v", err)
	}
}

func TestParseDeviceRoutingSetOK(t *testing.T) {
	got, err := parseDeviceRoutingSet([]interface{}{int32(0), int32(0), "set", "Drums"})
	if err != nil || got != "Drums" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
