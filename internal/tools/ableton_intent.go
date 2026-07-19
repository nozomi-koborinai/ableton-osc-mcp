package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

const intentVersion = 1

// IntentSetting is one human-readable parameter target within an intent.
type IntentSetting struct {
	Param string `json:"param" jsonschema:"description=Parameter name (e.g. Frequency) or numeric index"`
	Value string `json:"value" jsonschema:"description=Human-readable value (e.g. 180 Hz, HP, 60 %)"`
}

// Intent is a named recipe of parameter settings, applied on top of raw
// parameter tweaking to give an intent -> settings layer.
type Intent struct {
	Version  int             `json:"version"`
	Name     string          `json:"name"`
	Settings []IntentSetting `json:"settings"`
	SavedAt  time.Time       `json:"saved_at"`
}

var intentNameSanitize = regexp.MustCompile(`[^a-zA-Z0-9_.\- ]+`)

func intentDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ableton-osc-mcp", "intents")
}

func intentPath(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", errors.New("intent name is required")
	}
	clean = intentNameSanitize.ReplaceAllString(clean, "_")
	return filepath.Join(intentDir(), clean+".json"), nil
}

func writeIntent(path string, intent Intent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create intent dir: %w", err)
	}
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return fmt.Errorf("encode intent: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func readIntent(path string) (Intent, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Intent{}, fmt.Errorf("intent not found: %s", strings.TrimSuffix(filepath.Base(path), ".json"))
	}
	if err != nil {
		return Intent{}, fmt.Errorf("read intent: %w", err)
	}
	var intent Intent
	if err := json.Unmarshal(data, &intent); err != nil {
		return Intent{}, fmt.Errorf("parse intent: %w", err)
	}
	if intent.Version != intentVersion {
		return Intent{}, fmt.Errorf("unsupported intent version: %d", intent.Version)
	}
	return intent, nil
}

// resolveParamIndex maps a name or numeric string to a parameter index,
// trying exact, case-insensitive, then substring matches.
func resolveParamIndex(names []string, q string) (int, bool) {
	q = strings.TrimSpace(q)
	if q == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(q); err == nil {
		if n >= 0 && n < len(names) {
			return n, true
		}
		return 0, false
	}
	ql := strings.ToLower(q)
	for i, n := range names {
		if n == q {
			return i, true
		}
	}
	for i, n := range names {
		if strings.ToLower(n) == ql {
			return i, true
		}
	}
	for i, n := range names {
		if strings.Contains(strings.ToLower(n), ql) {
			return i, true
		}
	}
	return 0, false
}

type ApplyIntentResult struct {
	Param        string  `json:"param"`
	ParamIndex   int     `json:"param_index"`
	Requested    string  `json:"requested"`
	Status       string  `json:"status"`
	Value        float64 `json:"value,omitempty"`
	DisplayValue string  `json:"display_value,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type ApplyDeviceIntentInput struct {
	TrackIndex  int             `json:"track_index" jsonschema:"minimum=0"`
	DeviceIndex int             `json:"device_index" jsonschema:"minimum=0"`
	Name        string          `json:"name,omitempty" jsonschema:"description=Intent name. With settings: also save under this name. Without settings: load and apply this saved intent"`
	Settings    []IntentSetting `json:"settings,omitempty" jsonschema:"description=Inline parameter settings to apply (and save when name is given)"`
}

type ApplyDeviceIntentOutput struct {
	TrackIndex  int                 `json:"track_index"`
	DeviceIndex int                 `json:"device_index"`
	Name        string              `json:"name,omitempty"`
	Applied     int                 `json:"applied"`
	Failed      int                 `json:"failed"`
	Results     []ApplyIntentResult `json:"results"`
	SavedPath   string              `json:"saved_path,omitempty"`
}

func NewAbletonApplyDeviceIntent(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_apply_device_intent",
		"Ableton Live: apply a set of human-readable parameter settings (an intent, e.g. HP 180 Hz, or Beat Repeat Insert / 1/16 / Chance 60%) to a device in one call, resolving parameters by name. Provide inline settings and/or a name to save (with settings) or load+apply (without settings). Requires the browser patch",
		func(_ *ai.ToolContext, input ApplyDeviceIntentInput) (ApplyDeviceIntentOutput, error) {
			if input.TrackIndex < 0 || input.DeviceIndex < 0 {
				return ApplyDeviceIntentOutput{}, errors.New("track_index and device_index must be >= 0")
			}
			name := strings.TrimSpace(input.Name)
			settings := input.Settings
			saveIt := false
			switch {
			case len(settings) == 0 && name == "":
				return ApplyDeviceIntentOutput{}, errors.New("provide settings and/or a saved intent name")
			case len(settings) == 0:
				path, err := intentPath(name)
				if err != nil {
					return ApplyDeviceIntentOutput{}, err
				}
				loaded, err := readIntent(path)
				if err != nil {
					return ApplyDeviceIntentOutput{}, err
				}
				settings = loaded.Settings
			case name != "":
				saveIt = true
			}
			if len(settings) == 0 {
				return ApplyDeviceIntentOutput{}, errors.New("intent has no settings")
			}

			namesRes, err := client.Query("/live/device/get/parameters/name",
				int32(input.TrackIndex), int32(input.DeviceIndex))
			if err != nil {
				return ApplyDeviceIntentOutput{}, fmt.Errorf("get parameter names: %w", err)
			}
			if err := ensureResponseLen(namesRes, 2); err != nil {
				return ApplyDeviceIntentOutput{}, err
			}
			names := toStringSlice(namesRes[2:])

			out := ApplyDeviceIntentOutput{
				TrackIndex:  input.TrackIndex,
				DeviceIndex: input.DeviceIndex,
				Name:        name,
			}
			for _, s := range settings {
				r := ApplyIntentResult{Param: s.Param, Requested: s.Value, ParamIndex: -1}
				idx, ok := resolveParamIndex(names, s.Param)
				if !ok {
					r.Status = "unknown_param"
					r.Error = "no parameter matched"
					out.Failed++
					out.Results = append(out.Results, r)
					continue
				}
				r.ParamIndex = idx
				value := strings.TrimSpace(s.Value)
				res, err := client.QueryWithTimeout(3*time.Second, "/live/device/set/parameter/string",
					int32(input.TrackIndex), int32(input.DeviceIndex), int32(idx), value)
				if err != nil {
					r.Status = "error"
					r.Error = err.Error()
					out.Failed++
					out.Results = append(out.Results, r)
					continue
				}
				parsed, perr := parseSetDeviceParameterStringResponse(res)
				r.Status = parsed.Status
				if perr != nil {
					r.Error = perr.Error()
					out.Failed++
				} else {
					r.Value = parsed.Value
					r.DisplayValue = parsed.DisplayValue
					out.Applied++
				}
				out.Results = append(out.Results, r)
			}

			if saveIt {
				path, err := intentPath(name)
				if err != nil {
					return out, err
				}
				if err := writeIntent(path, Intent{
					Version:  intentVersion,
					Name:     name,
					Settings: settings,
					SavedAt:  time.Now().UTC(),
				}); err != nil {
					return out, err
				}
				out.SavedPath = path
			}
			return out, nil
		},
	)
}

type ListIntentsOutput struct {
	Dir     string   `json:"dir"`
	Intents []string `json:"intents"`
}

func NewAbletonListIntents(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_list_intents",
		"Ableton Live: list saved device intents (names) available for ableton_apply_device_intent.",
		func(_ *ai.ToolContext, _ struct{}) (ListIntentsOutput, error) {
			dir := intentDir()
			out := ListIntentsOutput{Dir: dir, Intents: []string{}}
			entries, err := os.ReadDir(dir)
			if errors.Is(err, os.ErrNotExist) {
				return out, nil
			}
			if err != nil {
				return ListIntentsOutput{}, fmt.Errorf("read intent dir: %w", err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				out.Intents = append(out.Intents, strings.TrimSuffix(e.Name(), ".json"))
			}
			sort.Strings(out.Intents)
			return out, nil
		},
	)
}
