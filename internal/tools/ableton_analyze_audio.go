package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/reference"
)

type AnalyzeLocalAudioInput struct {
	Path            string             `json:"path" jsonschema:"description=Absolute local path to a .wav\\, .aif or .aiff file you already have (no URLs)"`
	ProjectTempo    *float64           `json:"project_tempo,omitempty" jsonschema:"description=Optional project BPM to estimate length in bars,minimum=20,maximum=400"`
	StartSec        *float64           `json:"start_sec,omitempty" jsonschema:"description=Optional window start in seconds; everything reported then describes the window,minimum=0"`
	EndSec          *float64           `json:"end_sec,omitempty" jsonschema:"description=Optional window end in seconds\\, at least 1 s after start_sec,minimum=0"`
	Deep            bool               `json:"deep,omitempty" jsonschema:"description=Also work out a beat and bar grid\\, the chords on it (extended chords\\, bass\\, degree against the key\\, the loop they make) and the drum pattern of three bands on sixteenths. For studying a reference track; takes seconds longer and reads up to two minutes (choose them with start_sec/end_sec)"`
	DownbeatSec     *float64           `json:"downbeat_sec,omitempty" jsonschema:"description=With deep: time of a bar line in seconds from the start of the window\\, when it is known (0 for a bounce that starts on its bar line). Otherwise the downbeat is estimated\\, with a confidence,minimum=0"`
	References      []reference.Weight `json:"references,omitempty" jsonschema:"description=Saved reference profiles to compare mix_profile against\\, blended by weight"`
	SaveReferenceAs string             `json:"save_reference_as,omitempty" jsonschema:"description=Keep this file's numbers (never audio) as a reference profile under this name; 1-40 characters from a-z\\, 0-9\\, '-' and '_'. Writes to disk"`
}

type AnalyzeLocalAudioOutput struct {
	Path              string                         `json:"path"`
	Format            string                         `json:"format"`
	DurationSec       float64                        `json:"duration_sec"`
	SampleRate        int                            `json:"sample_rate"`
	Channels          int                            `json:"channels"`
	PeakLevel         float64                        `json:"peak_level"`
	RMSLevel          float64                        `json:"rms_level"`
	EstimatedBPM      float64                        `json:"estimated_bpm"`
	BPMConfidence     float64                        `json:"bpm_confidence"`
	BPMAlternatives   []audioanalyze.TempoHypothesis `json:"bpm_alternatives,omitempty"`
	OnsetCount        int                            `json:"onset_count"`
	Onsets            []audioanalyze.Onset           `json:"onsets,omitempty"`
	RhythmDensity     float64                        `json:"rhythm_density,omitempty"`
	RMSPerBeat        []float64                      `json:"rms_per_beat,omitempty"`
	BandBalance       *audioanalyze.BandBalance      `json:"band_balance,omitempty"`
	SuggestedWarpMode string                         `json:"suggested_warp_mode"`
	Key               string                         `json:"key,omitempty"`
	Scale             string                         `json:"scale,omitempty"`
	KeyConfidence     float64                        `json:"key_confidence,omitempty"`
	KeyAlternatives   []audioanalyze.KeyHypothesis   `json:"key_alternatives,omitempty"`
	Tuning            *audioanalyze.Tuning           `json:"tuning,omitempty"`
	ChordProgression  []audioanalyze.ChordSegment    `json:"chord_progression,omitempty"`
	ChordSummary      string                         `json:"chord_summary,omitempty"`
	Sections          []audioanalyze.Section         `json:"sections,omitempty"`
	MatchAxes         []audioanalyze.MatchAxis       `json:"match_axes,omitempty"`
	BrightnessHz      float64                        `json:"brightness_hz,omitempty"`
	CrestFactorDB     float64                        `json:"crest_factor_db,omitempty"`
	StereoWidth       float64                        `json:"stereo_width"`
	RangeStartSec     float64                        `json:"range_start_sec,omitempty"`
	RangeEndSec       float64                        `json:"range_end_sec,omitempty"`
	MixProfile        *audioanalyze.MixProfile       `json:"mix_profile,omitempty"`
	Grid              *audioanalyze.BeatGrid         `json:"grid,omitempty"`
	Harmony           *audioanalyze.Harmony          `json:"harmony,omitempty"`
	DrumGrid          *audioanalyze.DrumGrid         `json:"drum_grid,omitempty"`
	DeepNote          string                         `json:"deep_note,omitempty"`
	Reference         *reference.Comparison          `json:"reference,omitempty" jsonschema:"description=mix_profile measured against the blended references; differences only\\, no prescription"`
	SavedReference    string                         `json:"saved_reference,omitempty"`
	LengthBarsAtBPM   float64                        `json:"length_bars_at_project_tempo,omitempty"`
	Note              string                         `json:"note"`
	NextStep          string                         `json:"next_step"`
}

func NewAbletonAnalyzeLocalAudio(g *genkit.Genkit, store referenceStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_local_audio",
		"Analyze a local .wav/.aif for sampling placement and mix comparison: duration/levels, BPM (+ half/double alternatives), key/scale (+ alternative), chords, section map, onset grid {beat,sec,strength}, rhythm_density, rms_per_beat, band_balance, match_axes, texture, and mix_profile (integrated LUFS, true peak, crest, 9-band spectrum in dB, per-band stereo width). `references` compares mix_profile with saved reference profiles and returns per-band deltas. `save_reference_as` writes the numbers (never audio) to the reference profile file on disk — set it only when the person asked to keep this track as a reference. Key and chords are corrected for the track's tuning (`tuning`: cents away from A=440), which pitched-down references need. `deep` adds what one takes from a reference: a beat and bar `grid`, `harmony` (extended chords with bass, degree against the key, and the loop they make) and a `drum_grid` (kick/808, snare/clap, hats on sixteenths over two bars) — statistics folded over the track, from the full mix without stem separation, each with a confidence or a note on what blurs it; not a transcription. No URLs, no melody/note extraction.",
		func(_ *ai.ToolContext, input AnalyzeLocalAudioInput) (AnalyzeLocalAudioOutput, error) {
			return analyzeLocalAudio(input, store)
		},
	)
}

func analyzeLocalAudio(input AnalyzeLocalAudioInput, store referenceStore) (AnalyzeLocalAudioOutput, error) {
	saveAs, err := checkReferenceName(store, input.SaveReferenceAs)
	if err != nil {
		return AnalyzeLocalAudioOutput{}, err
	}
	refMix, blend, err := blendReferences(store, input.References)
	if err != nil {
		return AnalyzeLocalAudioOutput{}, err
	}

	got, err := audioanalyze.AnalyzeFile(input.Path, analysisOptions(input.ProjectTempo, input.StartSec, input.EndSec, input.Deep, input.DownbeatSec))
	if err != nil {
		return AnalyzeLocalAudioOutput{}, wrapUnsupportedFormat(err)
	}

	var comparison *reference.Comparison
	if refMix != nil && got.MixProfile != nil {
		c := reference.Compare(*got.MixProfile, *refMix, blend)
		comparison = &c
	}
	if saveAs != "" {
		if err := saveReference(store, saveAs, "file", got.Path, got); err != nil {
			return AnalyzeLocalAudioOutput{}, err
		}
	}
	return AnalyzeLocalAudioOutput{
		Path:              got.Path,
		Format:            got.Format,
		DurationSec:       got.DurationSec,
		SampleRate:        got.SampleRate,
		Channels:          got.Channels,
		PeakLevel:         got.PeakLevel,
		RMSLevel:          got.RMSLevel,
		EstimatedBPM:      got.EstimatedBPM,
		BPMConfidence:     got.BPMConfidence,
		BPMAlternatives:   got.BPMAlternatives,
		OnsetCount:        got.OnsetCount,
		Onsets:            got.Onsets,
		RhythmDensity:     got.RhythmDensity,
		RMSPerBeat:        got.RMSPerBeat,
		BandBalance:       got.BandBalance,
		SuggestedWarpMode: got.SuggestedWarpMode,
		Key:               got.Key,
		Scale:             got.Scale,
		KeyConfidence:     got.KeyConfidence,
		KeyAlternatives:   got.KeyAlternatives,
		Tuning:            got.Tuning,
		ChordProgression:  got.ChordProgression,
		ChordSummary:      got.ChordSummary,
		Sections:          got.Sections,
		MatchAxes:         got.MatchAxes,
		BrightnessHz:      got.BrightnessHz,
		CrestFactorDB:     got.CrestFactorDB,
		StereoWidth:       got.StereoWidth,
		RangeStartSec:     got.RangeStartSec,
		RangeEndSec:       got.RangeEndSec,
		MixProfile:        got.MixProfile,
		Grid:              got.Grid,
		Harmony:           got.Harmony,
		DrumGrid:          got.DrumGrid,
		DeepNote:          got.DeepNote,
		Reference:         comparison,
		SavedReference:    saveAs,
		LengthBarsAtBPM:   got.LengthBarsAtBPM,
		Note:              got.Note,
		NextStep:          "Use match_axes + band_balance for arrangement decisions; for chop placement use onsets/rms_per_beat. If loading into Live, call ableton_match_clip_tempo with suggested_warp_mode.",
	}, nil
}
