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

type DiagnoseOutput struct {
	Ready           bool                 `json:"ready" jsonschema:"description=true when AbletonOSC, browser patch, and master patch all respond"`
	Connected       bool                 `json:"connected" jsonschema:"description=true when stock AbletonOSC /live/test responds"`
	BrowserPatch    bool                 `json:"browser_patch"`
	MasterPatch     bool                 `json:"master_patch"`
	Config          DiagnoseConfigOutput `json:"config"`
	Checks          []DiagnoseCheck      `json:"checks"`
	Recommendations []string             `json:"recommendations"`
}

type diagnoseQuerier interface {
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonDiagnose(g *genkit.Genkit, client *abletonosc.Client, settings DiagnoseSettings) ai.Tool {
	return genkit.DefineTool(g, "ableton_diagnose",
		"Ableton Live: diagnose AbletonOSC connection and browser/master patch availability",
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
		Checks:          make([]DiagnoseCheck, 0, 3),
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

func formatProbeError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "no response received to query") {
		return "no response (timeout or endpoint missing)"
	}
	return msg
}

func buildDiagnoseRecommendations(out DiagnoseOutput) []string {
	recs := make([]string, 0, 4)

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
	if (!out.Connected || !out.BrowserPatch || !out.MasterPatch) && out.Config.TimeoutMs <= 500 {
		recs = append(recs,
			"If the set is heavy, try raising ABLETON_OSC_TIMEOUT_MS (for example 2000).",
		)
	}
	if out.Ready {
		recs = append(recs, "AbletonOSC and both patches look ready.")
	}
	return recs
}
