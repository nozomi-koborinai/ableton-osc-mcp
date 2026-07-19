package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

type AnalyzeLocalAudioInput struct {
	Path         string   `json:"path" jsonschema:"description=Absolute local path to a .wav file you already have (no URLs)"`
	ProjectTempo *float64 `json:"project_tempo,omitempty" jsonschema:"description=Optional project BPM to estimate length in bars,minimum=20,maximum=400"`
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
	ChordProgression  []audioanalyze.ChordSegment    `json:"chord_progression,omitempty"`
	ChordSummary      string                         `json:"chord_summary,omitempty"`
	Sections          []audioanalyze.Section         `json:"sections,omitempty"`
	MatchAxes         []audioanalyze.MatchAxis       `json:"match_axes,omitempty"`
	BrightnessHz      float64                        `json:"brightness_hz,omitempty"`
	CrestFactorDB     float64                        `json:"crest_factor_db,omitempty"`
	StereoWidth       float64                        `json:"stereo_width"`
	LengthBarsAtBPM   float64                        `json:"length_bars_at_project_tempo,omitempty"`
	Note              string                         `json:"note"`
	NextStep          string                         `json:"next_step"`
}

func NewAbletonAnalyzeLocalAudio(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_local_audio",
		"Analyze a local .wav for sampling placement: duration/levels, BPM (+ half/double alternatives), key/scale (+ alternative), chords, section map, onset grid {beat,sec,strength}, rhythm_density, rms_per_beat, band_balance (low/mid/high), match_axes (density/low-end/space), and texture (brightness/dynamics/stereo). No URLs, no melody/note extraction.",
		func(_ *ai.ToolContext, input AnalyzeLocalAudioInput) (AnalyzeLocalAudioOutput, error) {
			return analyzeLocalAudio(input)
		},
	)
}

func analyzeLocalAudio(input AnalyzeLocalAudioInput) (AnalyzeLocalAudioOutput, error) {
	projectTempo := 0.0
	if input.ProjectTempo != nil {
		projectTempo = *input.ProjectTempo
	}
	got, err := audioanalyze.AnalyzeFile(input.Path, projectTempo)
	if err != nil {
		return AnalyzeLocalAudioOutput{}, err
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
		ChordProgression:  got.ChordProgression,
		ChordSummary:      got.ChordSummary,
		Sections:          got.Sections,
		MatchAxes:         got.MatchAxes,
		BrightnessHz:      got.BrightnessHz,
		CrestFactorDB:     got.CrestFactorDB,
		StereoWidth:       got.StereoWidth,
		LengthBarsAtBPM:   got.LengthBarsAtBPM,
		Note:              got.Note,
		NextStep:          "Use match_axes + band_balance for arrangement decisions; for chop placement use onsets/rms_per_beat. If loading into Live, call ableton_match_clip_tempo with suggested_warp_mode.",
	}, nil
}
