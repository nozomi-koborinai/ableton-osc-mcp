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
		return nil, errors.New("unexpected query: " + address)
	}
	return result.values, result.err
}

func TestDiagnoseAbletonReady(t *testing.T) {
	t.Parallel()

	client := diagnoseQuerierStub{results: map[string]struct {
		values []interface{}
		err    error
	}{
		"/live/test": {values: []interface{}{"ok"}},
		"/live/browser/list_folder": {values: []interface{}{
			"roots",
			"Drums|loadable=False|folder=True",
			"Instruments|loadable=False|folder=True",
		}},
		"/live/master/get/volume": {values: []interface{}{float32(0.85)}},
	}}

	got := diagnoseAbleton(client, DiagnoseSettings{
		Host:       "127.0.0.1",
		Port:       11000,
		ClientPort: 11001,
		Timeout:    500 * time.Millisecond,
	})

	if !got.Ready || !got.Connected || !got.BrowserPatch || !got.MasterPatch {
		t.Fatalf("diagnoseAbleton() flags = ready=%v connected=%v browser=%v master=%v",
			got.Ready, got.Connected, got.BrowserPatch, got.MasterPatch)
	}
	if got.Config.TimeoutMs != 500 {
		t.Errorf("TimeoutMs = %d, want 500", got.Config.TimeoutMs)
	}
	if len(got.Checks) != 3 {
		t.Fatalf("Checks len = %d, want 3", len(got.Checks))
	}
	if len(got.Recommendations) == 0 || !strings.Contains(got.Recommendations[0], "ready") {
		t.Errorf("Recommendations = %v, want ready message", got.Recommendations)
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
		Timeout:    time.Second,
	})

	if got.Connected || got.Ready {
		t.Fatalf("expected connection failure, got connected=%v ready=%v", got.Connected, got.Ready)
	}
	joined := strings.Join(got.Recommendations, "\n")
	if !strings.Contains(joined, "Enable AbletonOSC") {
		t.Errorf("recommendations missing AbletonOSC guidance: %s", joined)
	}
	if strings.Contains(joined, "ABLETON_OSC_TIMEOUT_MS") {
		t.Errorf("timeout tip should be skipped when timeout > 500ms: %s", joined)
	}
}
