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
	Source            string                      `json:"source"`
	Format            string                      `json:"format"`
	DurationSec       float64                     `json:"duration_sec"`
	SampleRate        int                         `json:"sample_rate"`
	Channels          int                         `json:"channels"`
	PeakLevel         float64                     `json:"peak_level"`
	RMSLevel          float64                     `json:"rms_level"`
	EstimatedBPM      float64                     `json:"estimated_bpm"`
	BPMConfidence     float64                     `json:"bpm_confidence"`
	OnsetCount        int                         `json:"onset_count"`
	SuggestedWarpMode string                      `json:"suggested_warp_mode"`
	Key               string                      `json:"key,omitempty"`
	Scale             string                      `json:"scale,omitempty"`
	KeyConfidence     float64                     `json:"key_confidence,omitempty"`
	ChordProgression  []audioanalyze.ChordSegment `json:"chord_progression,omitempty"`
	ChordSummary      string                      `json:"chord_summary,omitempty"`
	Sections          []audioanalyze.Section      `json:"sections,omitempty"`
	LengthBarsAtBPM   float64                     `json:"length_bars_at_project_tempo,omitempty"`
	Note              string                      `json:"note"`
	NextStep          string                      `json:"next_step"`
}

func NewAbletonAnalyzeAudioURL(g *genkit.Genkit) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_audio_url",
		"Reference-analyze audio at an http(s) URL (e.g. YouTube) for tempo/length/levels, approximate key/scale, chord progression, and an energy-based section map. Streams the full track through yt-dlp+ffmpeg in memory, saves nothing, and never extracts melodies or notes. Requires yt-dlp and ffmpeg on PATH; you are responsible for your right to access the URL.",
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
				OnsetCount:        got.OnsetCount,
				SuggestedWarpMode: got.SuggestedWarpMode,
				Key:               got.Key,
				Scale:             got.Scale,
				KeyConfidence:     got.KeyConfidence,
				ChordProgression:  got.ChordProgression,
				ChordSummary:      got.ChordSummary,
				Sections:          got.Sections,
				LengthBarsAtBPM:   got.LengthBarsAtBPM,
				Note:              got.Note,
				NextStep:          "Use the tempo/key/chord progression as a reference to build your own part. This tool does not import the audio; place only samples you have the right to use.",
			}, nil
		},
	)
}
