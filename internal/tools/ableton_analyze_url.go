package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

type AnalyzeAudioURLInput struct {
	URL          string   `json:"url" jsonschema:"description=http(s) URL to reference (e.g. YouTube). Streamed briefly for analysis and never saved."`
	ProjectTempo *float64 `json:"project_tempo,omitempty" jsonschema:"description=Optional project BPM to estimate length in bars,minimum=20,maximum=400"`
}

type AnalyzeAudioURLOutput struct {
	Source            string                         `json:"source"`
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

func NewAbletonAnalyzeAudioURL(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_audio_url",
		"Reference-analyze audio at an http(s) URL (e.g. YouTube) for tempo (+ half/double alternatives), key/scale (+ alternative), chords, section map, rhythm_density, rms_per_beat, band_balance, match_axes (density/low-end/space), and texture. Streams via yt-dlp+ffmpeg in memory, saves nothing, never extracts melodies/notes. Requires yt-dlp and ffmpeg on PATH; you are responsible for your right to access the URL.",
		func(tc *ai.ToolContext, input AnalyzeAudioURLInput) (AnalyzeAudioURLOutput, error) {
			projectTempo := 0.0
			if input.ProjectTempo != nil {
				projectTempo = *input.ProjectTempo
			}
			got, err := audioanalyze.AnalyzeURL(tc, input.URL, projectTempo)
			if err != nil {
				return AnalyzeAudioURLOutput{}, err
			}
			return AnalyzeAudioURLOutput{
				Source:            got.Path,
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
				NextStep:          "Use match_axes + band_balance + section map as arrangement cues; build your own part from tempo/key/chords. This tool does not import audio — place only samples you have rights to use.",
			}, nil
		},
	)
}
