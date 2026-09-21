package tools

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

const (
	measureTrackName   = "Measure"
	measureDefaultBars = 8
	measureMaxGroups   = 6
	measureSilenceDBFS = -80.0
	measureSquashedDB  = 8.0 // a full mix with less crest than this is being limited hard
)

type MeasureGroup struct {
	Name         string `json:"name" jsonschema:"description=Label for the group\\, e.g. drums"`
	TrackIndices []int  `json:"track_indices" jsonschema:"description=Tracks soloed together for this group's pass"`
}

type MeasureMixInput struct {
	SceneIndex    *int               `json:"scene_index,omitempty" jsonschema:"description=Scene to fire for the pass. Omit to measure whatever is playing,minimum=0"`
	Bars          int                `json:"bars,omitempty" jsonschema:"description=Bars to record (default 8). The call takes this long in real time,minimum=1,maximum=64"`
	References    []reference.Weight `json:"references,omitempty" jsonschema:"description=Saved reference profiles to compare against\\, blended by weight"`
	Groups        []MeasureGroup     `json:"groups,omitempty" jsonschema:"description=Optional track groups (e.g. drums\\, 808\\, tops). Each adds one more real-time pass with those tracks soloed,maxItems=6"`
	KeepRecording bool               `json:"keep_recording,omitempty" jsonschema:"description=Leave the recorded clip on the Measure track instead of deleting it"`
}

type MeasuredGroup struct {
	Name         string                  `json:"name"`
	TrackIndices []int                   `json:"track_indices"`
	LUFS         float64                 `json:"lufs_integrated"`
	LevelVsMixDB float64                 `json:"level_vs_mix_db" jsonschema:"description=Group loudness minus full-mix loudness"`
	Mix          audioanalyze.MixProfile `json:"mix"`
}

type MeasureMixOutput struct {
	Mix           audioanalyze.MixProfile `json:"mix"`
	Groups        []MeasuredGroup         `json:"groups,omitempty"`
	Reference     *reference.Comparison   `json:"reference,omitempty" jsonschema:"description=The full mix against the blended references; differences only\\, no prescription"`
	RecordedFiles []string                `json:"recorded_files" jsonschema:"description=Files Live wrote for the passes; Live does not delete them"`
	DurationSec   float64                 `json:"duration_sec"`
	Note          string                  `json:"note"`
}

type measureDeps struct {
	record   recordDeps
	analyze  func(path string, opts audioanalyze.Options) (audioanalyze.Result, error)
	duration func(path string) (float64, error)
	store    referenceStore
}

func NewAbletonMeasureMix(g *genkit.Genkit, client *abletonosc.Client, store referenceStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_measure_mix",
		"Ableton Live: record what Live is putting out (Resampling onto a muted 'Measure' audio track) and measure it — integrated LUFS, true peak, crest, 9-band spectrum, per-band stereo width — optionally against saved reference profiles and per track group. Runs in real time: `bars` bars (bar to bar, so up to one bar of waiting first), plus one more pass per group. It creates the Measure track if missing, leaves the audio files in the Live project's recordings folder, and deletes its own clips unless keep_recording is set. For the length of a pass it disarms any other armed track and selects an unused scene row (Session Record would otherwise record on those tracks too); both are put back afterwards. Use when the listener asks to measure the mix, or to check a mix change against the references; not for a level check of one track (ableton_get_track_meter). It reports differences and never moves a fader or an EQ.",
		func(_ *ai.ToolContext, input MeasureMixInput) (MeasureMixOutput, error) {
			return measureMixTool(client, measureDeps{
				record: recordDeps{
					sleep: time.Sleep,
					fileSize: func(path string) (int64, error) {
						info, err := os.Stat(path)
						if err != nil {
							return 0, err
						}
						return info.Size(), nil
					},
				},
				analyze:  audioanalyze.AnalyzeFile,
				duration: audioanalyze.ProbeDuration,
				store:    store,
			}, input)
		},
	)
}

func measureMixTool(c recordClient, deps measureDeps, input MeasureMixInput) (MeasureMixOutput, error) {
	bars := input.Bars
	if bars == 0 {
		bars = measureDefaultBars
	}
	if bars < 1 || bars > recordMaxBars {
		return MeasureMixOutput{}, fmt.Errorf("bars must be between 1 and %d", recordMaxBars)
	}
	if input.SceneIndex != nil && *input.SceneIndex < 0 {
		return MeasureMixOutput{}, errors.New("scene_index must be >= 0")
	}
	if len(input.Groups) > measureMaxGroups {
		return MeasureMixOutput{}, fmt.Errorf("at most %d groups (each one is another real-time pass)", measureMaxGroups)
	}
	for _, g := range input.Groups {
		if strings.TrimSpace(g.Name) == "" || len(g.TrackIndices) == 0 {
			return MeasureMixOutput{}, errors.New("every group needs a name and at least one track index")
		}
		for _, t := range g.TrackIndices {
			if t < 0 {
				return MeasureMixOutput{}, errors.New("group track_indices must be >= 0")
			}
		}
	}
	// Resolve the references first: a typo should not cost a real-time pass.
	refMix, blend, err := blendReferences(deps.store, input.References)
	if err != nil {
		return MeasureMixOutput{}, err
	}

	started := time.Now()
	out := MeasureMixOutput{RecordedFiles: []string{}}
	pass := func(sceneIndex *int) (audioanalyze.MixProfile, error) {
		mix, path, err := measureOnePass(c, deps, sceneIndex, bars, input.KeepRecording)
		if path != "" {
			out.RecordedFiles = append(out.RecordedFiles, path)
		}
		return mix, err
	}

	mix, err := pass(input.SceneIndex)
	if err != nil {
		return MeasureMixOutput{}, err
	}
	out.Mix = mix

	if len(input.Groups) > 0 {
		groups, err := measureGroups(c, input, pass)
		if err != nil {
			return MeasureMixOutput{}, err
		}
		for i := range groups {
			groups[i].LevelVsMixDB = math.Round((groups[i].LUFS-mix.LUFSIntegrated)*100) / 100
		}
		out.Groups = groups
	}

	if refMix != nil {
		comparison := reference.Compare(mix, *refMix, blend)
		out.Reference = &comparison
	}
	out.DurationSec = math.Round(time.Since(started).Seconds()*10) / 10
	out.Note = measureNote(mix)
	return out, nil
}

// measureOnePass records one window, measures it, and deletes the clip again
// (unless asked to keep it) whether or not the measurement worked.
func measureOnePass(c recordClient, deps measureDeps, sceneIndex *int, bars int, keep bool) (audioanalyze.MixProfile, string, error) {
	take, err := recordResampledPass(c, deps.record, recordPlan{
		TrackName: measureTrackName,
		Spans:     []recordSpan{{SceneIndex: sceneIndex, Bars: bars}},
	})
	if !keep {
		defer func() {
			for _, slot := range take.Slots {
				_ = c.Send("/live/clip_slot/delete_clip", int32(take.TrackIndex), int32(slot))
			}
		}()
	}
	if err != nil {
		return audioanalyze.MixProfile{}, "", err
	}
	if len(take.FilePaths) != 1 {
		return audioanalyze.MixProfile{}, "", actionable("recording_split",
			fmt.Sprintf("Live split the pass into %d clips, so there is no single file to measure", len(take.FilePaths)),
			"In Live's Record/Warp/Launch preferences, turn off 'Start Recording on Scene Launch', then try again.")
	}
	path := take.FilePaths[0]

	total, err := deps.duration(path)
	if err != nil {
		return audioanalyze.MixProfile{}, path, err
	}
	startSec, endSec, err := locateWindow(total, take)
	if err != nil {
		return audioanalyze.MixProfile{}, path, err
	}
	res, err := deps.analyze(path, audioanalyze.Options{StartSec: startSec, EndSec: endSec})
	if err != nil {
		return audioanalyze.MixProfile{}, path, err
	}
	if res.MixProfile == nil || res.MixProfile.SamplePeakDBFS <= measureSilenceDBFS {
		return audioanalyze.MixProfile{}, path, actionable("recording_silent",
			"the pass recorded silence",
			"Check that sound reaches the master and that no other track is soloed, then try again.")
	}
	return *res.MixProfile, path, nil
}

// measureGroups solos each group in turn and measures it, then puts every solo
// switch back the way the listener had it.
func measureGroups(c recordClient, input MeasureMixInput, pass func(*int) (audioanalyze.MixProfile, error)) ([]MeasuredGroup, error) {
	res, err := c.Query("/live/song/get/track_data", int32(0), int32(-1), "track.solo")
	if err != nil {
		return nil, fmt.Errorf("read solo states: %w", err)
	}
	original := make([]bool, len(res))
	for i, v := range res {
		if original[i], err = asBoolish(v); err != nil {
			return nil, err
		}
	}
	current := append([]bool{}, original...)
	setSolo := func(want func(track int) bool) error {
		for track := range current {
			if w := want(track); w != current[track] {
				value := int32(0)
				if w {
					value = 1
				}
				if err := c.Send("/live/track/set/solo", int32(track), value); err != nil {
					return err
				}
				current[track] = w
			}
		}
		return nil
	}
	defer func() { _ = setSolo(func(track int) bool { return original[track] }) }()

	groups := make([]MeasuredGroup, 0, len(input.Groups))
	for _, g := range input.Groups {
		members := make(map[int]bool, len(g.TrackIndices))
		for _, t := range g.TrackIndices {
			if t >= len(current) {
				return nil, fmt.Errorf("group %q: no track at index %d", g.Name, t)
			}
			members[t] = true
		}
		if err := setSolo(func(track int) bool { return members[track] }); err != nil {
			return nil, err
		}
		mix, err := pass(input.SceneIndex)
		if err != nil {
			return nil, fmt.Errorf("group %q: %w", g.Name, err)
		}
		groups = append(groups, MeasuredGroup{Name: g.Name, TrackIndices: g.TrackIndices, LUFS: mix.LUFSIntegrated, Mix: mix})
	}
	return groups, nil
}

func measureNote(mix audioanalyze.MixProfile) string {
	note := "Measured from a Resampling recording of Live's master output. The audio files stay in the Live project's recordings folder; Live does not delete them."
	if mix.CrestDB < measureSquashedDB {
		note = fmt.Sprintf("Crest is %.1f dB: the master is being limited hard. While that lasts, fader moves barely reach the output, so check the input stage of the master chain before balancing. ", mix.CrestDB) + note
	}
	return note
}
