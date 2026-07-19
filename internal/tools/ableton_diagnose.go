package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type DiagnoseSettings struct {
	Host       string
	Port       int
	ClientPort int
	Timeout    time.Duration
}

type DiagnoseConfigOutput struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	ClientPort int    `json:"client_port"`
	TimeoutMs  int    `json:"timeout_ms"`
}

type DiagnoseCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type LiveVersionInfo struct {
	Major int    `json:"major"`
	Minor int    `json:"minor"`
	Label string `json:"label" jsonschema:"description=e.g. 11.0 (bugfix not reported by Live's API)"`
}

type CapabilityInfo struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	NextStep string `json:"next_step,omitempty" jsonschema:"description=What to do when ok=false"`
}

type DiagnoseOutput struct {
	Ready           bool                 `json:"ready" jsonschema:"description=true when AbletonOSC, browser patch, and master patch all respond"`
	Connected       bool                 `json:"connected" jsonschema:"description=true when stock AbletonOSC /live/test responds"`
	BrowserPatch    bool                 `json:"browser_patch"`
	MasterPatch     bool                 `json:"master_patch"`
	LiveVersion     *LiveVersionInfo     `json:"live_version,omitempty"`
	Capabilities    []CapabilityInfo     `json:"capabilities" jsonschema:"description=Feature availability so agents can avoid unsupported paths before they fail"`
	Config          DiagnoseConfigOutput `json:"config"`
	Checks          []DiagnoseCheck      `json:"checks"`
	Recommendations []string             `json:"recommendations"`
}

type diagnoseQuerier interface {
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonDiagnose(g *genkit.Genkit, client *abletonosc.Client, settings DiagnoseSettings) ai.Tool {
	return genkit.DefineTool(g, "ableton_diagnose",
		"Ableton Live: diagnose AbletonOSC connection, browser/master patches, Live version, and feature capabilities (e.g. create_audio_clip needs Live 12.0.5+)",
		func(_ *ai.ToolContext, _ EmptyInput) (DiagnoseOutput, error) {
			return diagnoseAbleton(client, settings), nil
		},
	)
}

func diagnoseAbleton(client diagnoseQuerier, settings DiagnoseSettings) DiagnoseOutput {
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}

	out := DiagnoseOutput{
		Config: DiagnoseConfigOutput{
			Host:       settings.Host,
			Port:       settings.Port,
			ClientPort: settings.ClientPort,
			TimeoutMs:  int(timeout / time.Millisecond),
		},
		Checks:          make([]DiagnoseCheck, 0, 4),
		Capabilities:    []CapabilityInfo{},
		Recommendations: []string{},
	}

	oscCheck := probeAbletonOSC(client)
	out.Checks = append(out.Checks, oscCheck)
	out.Connected = oscCheck.OK

	browserCheck := probeBrowserPatch(client)
	out.Checks = append(out.Checks, browserCheck)
	out.BrowserPatch = browserCheck.OK

	masterCheck := probeMasterPatch(client)
	out.Checks = append(out.Checks, masterCheck)
	out.MasterPatch = masterCheck.OK

	if out.Connected {
		if ver, check := probeLiveVersion(client); ver != nil {
			out.LiveVersion = ver
			out.Checks = append(out.Checks, check)
		} else {
			out.Checks = append(out.Checks, check)
		}
	}

	out.Capabilities = probeCapabilities(client, out)
	out.Ready = out.Connected && out.BrowserPatch && out.MasterPatch
	out.Recommendations = buildDiagnoseRecommendations(out)
	return out
}

func probeAbletonOSC(client diagnoseQuerier) DiagnoseCheck {
	res, err := client.Query("/live/test")
	if err != nil {
		return DiagnoseCheck{
			Name:   "ableton_osc",
			OK:     false,
			Detail: formatProbeError(err),
		}
	}
	detail := "ok"
	if len(res) > 0 {
		detail = fmt.Sprint(res[0])
	}
	return DiagnoseCheck{Name: "ableton_osc", OK: true, Detail: detail}
}

func probeBrowserPatch(client diagnoseQuerier) DiagnoseCheck {
	res, err := client.Query("/live/browser/list_folder")
	if err != nil {
		return DiagnoseCheck{
			Name:   "browser_patch",
			OK:     false,
			Detail: formatProbeError(err),
		}
	}
	if len(res) == 0 {
		return DiagnoseCheck{
			Name:   "browser_patch",
			OK:     false,
			Detail: "empty reply from /live/browser/list_folder",
		}
	}
	if fmt.Sprint(res[0]) != "roots" {
		return DiagnoseCheck{
			Name:   "browser_patch",
			OK:     false,
			Detail: fmt.Sprintf("unexpected reply: %v", res),
		}
	}
	return DiagnoseCheck{
		Name:   "browser_patch",
		OK:     true,
		Detail: fmt.Sprintf("roots=%d", len(res)-1),
	}
}

func probeMasterPatch(client diagnoseQuerier) DiagnoseCheck {
	res, err := client.Query("/live/master/get/volume")
	if err != nil {
		return DiagnoseCheck{
			Name:   "master_patch",
			OK:     false,
			Detail: formatProbeError(err),
		}
	}
	if err := ensureResponseLen(res, 1); err != nil {
		return DiagnoseCheck{
			Name:   "master_patch",
			OK:     false,
			Detail: err.Error(),
		}
	}
	volume, err := abletonosc.AsFloat64(res[0])
	if err != nil {
		return DiagnoseCheck{
			Name:   "master_patch",
			OK:     false,
			Detail: err.Error(),
		}
	}
	return DiagnoseCheck{
		Name:   "master_patch",
		OK:     true,
		Detail: fmt.Sprintf("volume=%.3f", volume),
	}
}

func probeLiveVersion(client diagnoseQuerier) (*LiveVersionInfo, DiagnoseCheck) {
	res, err := client.Query("/live/application/get/version")
	if err != nil {
		return nil, DiagnoseCheck{
			Name:   "live_version",
			OK:     false,
			Detail: formatProbeError(err),
		}
	}
	if err := ensureResponseLen(res, 2); err != nil {
		return nil, DiagnoseCheck{Name: "live_version", OK: false, Detail: err.Error()}
	}
	major, err := abletonosc.AsInt(res[0])
	if err != nil {
		return nil, DiagnoseCheck{Name: "live_version", OK: false, Detail: err.Error()}
	}
	minor, err := abletonosc.AsInt(res[1])
	if err != nil {
		return nil, DiagnoseCheck{Name: "live_version", OK: false, Detail: err.Error()}
	}
	ver := &LiveVersionInfo{
		Major: major,
		Minor: minor,
		Label: fmt.Sprintf("%d.%d", major, minor),
	}
	return ver, DiagnoseCheck{Name: "live_version", OK: true, Detail: ver.Label}
}

// handlerPresent treats a quick reply (including missing_args / error) as proof
// the OSC handler is registered. Timeout/no-response means missing.
func handlerPresent(client diagnoseQuerier, address string) bool {
	_, err := client.Query(address)
	if err == nil {
		return true
	}
	msg := err.Error()
	return !strings.Contains(msg, "no response received to query")
}

func probeCapabilities(client diagnoseQuerier, diag DiagnoseOutput) []CapabilityInfo {
	caps := make([]CapabilityInfo, 0, 8)

	// create_audio_clip: handler in browser patch; Live API only on 12.0.5+.
	// Live only reports major.minor, so we treat major>=12 as "likely".
	createHandler := diag.BrowserPatch && handlerPresent(client, "/live/clip_slot/create_audio_clip")
	createAPI := diag.LiveVersion != nil && diag.LiveVersion.Major >= 12
	switch {
	case createHandler && createAPI:
		caps = append(caps, CapabilityInfo{
			Name:   "create_audio_clip",
			OK:     true,
			Detail: "handler present; Live " + diag.LiveVersion.Label + " (needs 12.0.5+ bugfix)",
		})
	case createHandler && !createAPI:
		label := "unknown"
		if diag.LiveVersion != nil {
			label = diag.LiveVersion.Label
		}
		caps = append(caps, CapabilityInfo{
			Name:     "create_audio_clip",
			OK:       false,
			Detail:   "handler present but Live " + label + " lacks ClipSlot.create_audio_clip",
			NextStep: "Use ableton_load_browser_path onto an audio track, or upgrade to Live 12.0.5+.",
		})
	default:
		caps = append(caps, CapabilityInfo{
			Name:     "create_audio_clip",
			OK:       false,
			Detail:   "browser patch handler missing or AbletonOSC not connected",
			NextStep: "Install remote-script/ browser patch and restart Live.",
		})
	}

	type patchCap struct {
		name, address, next string
	}
	for _, p := range []patchCap{
		{"parameter_value_string", "/live/device/get/parameters/value_string", "Install/update the browser patch (value_string handler), then restart Live."},
		{"parameter_set_string", "/live/device/set/parameter/string", "Install/update the browser patch (set/parameter/string), then restart Live."},
		{"delete_device_confirmed", "/live/device/delete", "Install/update the browser patch (device/delete), then restart Live."},
		{"simpler_control", "/live/device/simpler/get", "Install/update the browser patch (simpler handlers), then restart Live."},
		{"slice_preset_restore", "/live/device/simpler/set/slices", "Install/update the browser patch (simpler/set/slices), then restart Live."},
		{"return_tracks_list", "/live/song/get/return_tracks", "Install/update the browser patch (return_tracks handler), then restart or hot-reload AbletonOSC."},
		{"device_sidechain", "/live/device/get/available_input_routing_types", "Install/update the browser patch (device sidechain handlers), then restart or hot-reload AbletonOSC."},
		{"clip_envelope", "/live/clip/envelope/get", "Install/update the browser patch (clip envelope handlers), then restart or hot-reload AbletonOSC."},
		{"device_is_active", "/live/device/get/is_active", "Install/update the browser patch (device is_active handlers), then restart or hot-reload AbletonOSC."},
	} {
		ok := diag.BrowserPatch && handlerPresent(client, p.address)
		info := CapabilityInfo{Name: p.name, OK: ok}
		if ok {
			info.Detail = "handler present"
		} else {
			info.Detail = "handler missing or patch not loaded"
			info.NextStep = p.next
		}
		caps = append(caps, info)
	}

	// Explicitly document Live API impossibility (removed after validation).
	caps = append(caps, CapabilityInfo{
		Name:     "drum_pad_sample_load",
		OK:       false,
		Detail:   "not available: Live 11 LOM cannot load a raw sample onto a specific Drum Rack pad without replacing the rack",
		NextStep: "Load a kit with filled pads in Live, or drag samples onto pads manually.",
	})

	return caps
}

func formatProbeError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no response received to query") {
		return "no response (timeout or endpoint missing)"
	}
	return msg
}

func buildDiagnoseRecommendations(out DiagnoseOutput) []string {
	recs := make([]string, 0, 6)

	if !out.Connected {
		recs = append(recs,
			"Enable AbletonOSC under Preferences → Link/Tempo/MIDI → Control Surface, then fully restart Ableton Live.",
			fmt.Sprintf("Confirm Live is reachable at %s:%d (replies to client port %d).", out.Config.Host, out.Config.Port, out.Config.ClientPort),
		)
	}
	if out.Connected && !out.BrowserPatch {
		recs = append(recs,
			"Install the browser patch from remote-script/ (copy browser.py and apply manager patch), then fully restart Ableton Live.",
		)
	}
	if out.Connected && !out.MasterPatch {
		recs = append(recs,
			"Install the master patch from remote-script/ (copy master.py and apply manager patch), then fully restart Ableton Live.",
		)
	}
	if out.LiveVersion != nil && out.LiveVersion.Major < 12 {
		recs = append(recs,
			fmt.Sprintf("Live %s: create_audio_clip and Splice path-load into Session slots are unavailable — prefer browser load or upgrade to Live 12.0.5+.", out.LiveVersion.Label),
		)
	}
	for _, c := range out.Capabilities {
		if !c.OK && c.Name == "create_audio_clip" && c.NextStep != "" && out.LiveVersion != nil && out.LiveVersion.Major < 12 {
			// already covered above
			break
		}
	}
	if (!out.Connected || !out.BrowserPatch || !out.MasterPatch) && out.Config.TimeoutMs <= 500 {
		recs = append(recs,
			"If the set is heavy, try raising ABLETON_OSC_TIMEOUT_MS (for example 2000).",
		)
	}
	if out.Ready {
		recs = append(recs, "AbletonOSC and both patches look ready. Check capabilities[] before using Live-version-gated features.")
	}
	return recs
}
