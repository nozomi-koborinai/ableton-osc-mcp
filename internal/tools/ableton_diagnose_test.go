package tools

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type diagnoseQuerierStub struct {
	results map[string]struct {
		values []interface{}
		err    error
	}
}

func (s diagnoseQuerierStub) Query(address string, _ ...interface{}) ([]interface{}, error) {
	result, ok := s.results[address]
	if !ok {
		// Unknown address → simulate missing handler (no response).
		return nil, errors.New("no response received to query: " + address)
	}
	return result.values, result.err
}

func readyDiagnoseStub() diagnoseQuerierStub {
	missingArgs := []interface{}{"error", "missing_args"}
	return diagnoseQuerierStub{results: map[string]struct {
		values []interface{}
		err    error
	}{
		"/live/test": {values: []interface{}{"ok"}},
		"/live/browser/list_folder": {values: []interface{}{
			"roots",
			"Drums|loadable=False|folder=True",
			"Instruments|loadable=False|folder=True",
		}},
		"/live/master/get/volume":                        {values: []interface{}{float32(0.85)}},
		"/live/application/get/version":                  {values: []interface{}{int32(11), int32(0)}},
		"/live/clip_slot/create_audio_clip":              {values: missingArgs},
		"/live/device/get/parameters/value_string":       {values: missingArgs},
		"/live/device/set/parameter/string":              {values: missingArgs},
		"/live/device/delete":                            {values: missingArgs},
		"/live/device/simpler/get":                       {values: missingArgs},
		"/live/device/simpler/set/slices":                {values: missingArgs},
		"/live/song/get/return_tracks":                   {values: []interface{}{int32(0)}},
		"/live/device/get/available_input_routing_types": {values: missingArgs},
		"/live/clip/envelope/get":                        {values: missingArgs},
	}}
}

func TestDiagnoseAbletonReady(t *testing.T) {
	t.Parallel()

	got := diagnoseAbleton(readyDiagnoseStub(), DiagnoseSettings{
		Host:       "127.0.0.1",
		Port:       11000,
		ClientPort: 11001,
		Timeout:    500 * time.Millisecond,
	})

	if !got.Ready || !got.Connected || !got.BrowserPatch || !got.MasterPatch {
		t.Fatalf("diagnoseAbleton() flags = ready=%v connected=%v browser=%v master=%v",
			got.Ready, got.Connected, got.BrowserPatch, got.MasterPatch)
	}
	if got.LiveVersion == nil || got.LiveVersion.Label != "11.0" {
		t.Fatalf("LiveVersion = %+v, want 11.0", got.LiveVersion)
	}
	if got.Config.TimeoutMs != 500 {
		t.Errorf("TimeoutMs = %d, want 500", got.Config.TimeoutMs)
	}
	if len(got.Checks) < 4 {
		t.Fatalf("Checks len = %d, want >= 4", len(got.Checks))
	}

	caps := map[string]CapabilityInfo{}
	for _, c := range got.Capabilities {
		caps[c.Name] = c
	}
	if caps["create_audio_clip"].OK {
		t.Error("create_audio_clip should be false on Live 11")
	}
	if caps["create_audio_clip"].NextStep == "" {
		t.Error("create_audio_clip should include next_step")
	}
	if !caps["simpler_control"].OK {
		t.Error("simpler_control should be present")
	}
	if caps["drum_pad_sample_load"].OK {
		t.Error("drum_pad_sample_load must stay unavailable")
	}

	joined := strings.Join(got.Recommendations, "\n")
	if !strings.Contains(joined, "create_audio_clip") && !strings.Contains(joined, "12.0.5") {
		t.Errorf("recommendations should mention Live 11 limitation: %s", joined)
	}
}

func TestDiagnoseAbletonReportsMissingPieces(t *testing.T) {
	t.Parallel()

	client := diagnoseQuerierStub{results: map[string]struct {
		values []interface{}
		err    error
	}{
		"/live/test": {values: []interface{}{"ok"}},
		"/live/browser/list_folder": {
			err: errors.New("no response received to query: /live/browser/list_folder"),
		},
		"/live/master/get/volume": {
			err: errors.New("no response received to query: /live/master/get/volume"),
		},
		"/live/application/get/version": {values: []interface{}{int32(11), int32(0)}},
	}}

	got := diagnoseAbleton(client, DiagnoseSettings{
		Host:       "127.0.0.1",
		Port:       11000,
		ClientPort: 11001,
		Timeout:    500 * time.Millisecond,
	})

	if got.Ready {
		t.Fatal("Ready = true, want false")
	}
	if !got.Connected {
		t.Fatal("Connected = false, want true")
	}
	if got.BrowserPatch || got.MasterPatch {
		t.Fatalf("patches should fail: browser=%v master=%v", got.BrowserPatch, got.MasterPatch)
	}

	joined := strings.Join(got.Recommendations, "\n")
	if !strings.Contains(joined, "browser patch") {
		t.Errorf("recommendations missing browser guidance: %s", joined)
	}
	if !strings.Contains(joined, "master patch") {
		t.Errorf("recommendations missing master guidance: %s", joined)
	}
	if !strings.Contains(joined, "ABLETON_OSC_TIMEOUT_MS") {
		t.Errorf("recommendations missing timeout guidance: %s", joined)
	}
}

func TestDiagnoseAbletonConnectionFailure(t *testing.T) {
	t.Parallel()

	client := diagnoseQuerierStub{results: map[string]struct {
		values []interface{}
		err    error
	}{
		"/live/test": {err: errors.New("no response received to query: /live/test")},
		"/live/browser/list_folder": {
			err: errors.New("no response received to query: /live/browser/list_folder"),
		},
		"/live/master/get/volume": {
			err: errors.New("no response received to query: /live/master/get/volume"),
		},
	}}

	got := diagnoseAbleton(client, DiagnoseSettings{
		Host:       "127.0.0.1",
		Port:       11000,
		ClientPort: 11001,
		Timeout:    500 * time.Millisecond,
	})

	if got.Connected || got.Ready {
		t.Fatalf("should be disconnected: connected=%v ready=%v", got.Connected, got.Ready)
	}
	joined := strings.Join(got.Recommendations, "\n")
	if !strings.Contains(joined, "Control Surface") {
		t.Errorf("recommendations missing connection guidance: %s", joined)
	}
}

func TestDiagnoseCreateAudioClipOnLive12(t *testing.T) {
	t.Parallel()
	stub := readyDiagnoseStub()
	stub.results["/live/application/get/version"] = struct {
		values []interface{}
		err    error
	}{values: []interface{}{int32(12), int32(1)}}

	got := diagnoseAbleton(stub, DiagnoseSettings{Timeout: 500 * time.Millisecond})
	for _, c := range got.Capabilities {
		if c.Name == "create_audio_clip" && !c.OK {
			t.Fatalf("create_audio_clip should be ok on Live 12: %+v", c)
		}
	}
}
