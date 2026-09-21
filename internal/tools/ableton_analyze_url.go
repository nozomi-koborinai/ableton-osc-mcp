package tools

import (
	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/audioanalyze"
)

type AnalyzeAudioURLInput struct {
	URL             string   `json:"url" jsonschema:"description=http(s) URL to reference (e.g. YouTube). Streamed briefly for analysis and never saved."`
	ProjectTempo    *float64 `json:"project_tempo,omitempty" jsonschema:"description=Optional project BPM to estimate length in bars,minimum=20,maximum=400"`
	StartSec        *float64 `json:"start_sec,omitempty" jsonschema:"description=Optional window start in seconds (e.g. to skip a long intro); everything reported then describes the window,minimum=0"`
	EndSec          *float64 `json:"end_sec,omitempty" jsonschema:"description=Optional window end in seconds\\, at least 1 s after start_sec,minimum=0"`
	SaveReferenceAs string   `json:"save_reference_as,omitempty" jsonschema:"description=Keep this track's numbers (never audio) as a reference profile under this name; 1-40 characters from a-z\\, 0-9\\, '-' and '_'. Writes to disk"`
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
	RangeStartSec     float64                        `json:"range_start_sec,omitempty"`
	RangeEndSec       float64                        `json:"range_end_sec,omitempty"`
	MixProfile        *audioanalyze.MixProfile       `json:"mix_profile,omitempty"`
	SavedReference    string                         `json:"saved_reference,omitempty"`
	LengthBarsAtBPM   float64                        `json:"length_bars_at_project_tempo,omitempty"`
	Note              string                         `json:"note"`
	NextStep          string                         `json:"next_step"`
}

func NewAbletonAnalyzeAudioURL(g *genkit.Genkit, store referenceStore) ai.Tool {
	return genkit.DefineTool(g, "ableton_analyze_audio_url",
		"Reference-analyze audio at an http(s) URL (e.g. YouTube) for tempo (+ half/double alternatives), key/scale (+ alternative), chords, section map, rhythm_density, rms_per_beat, band_balance, match_axes, texture, and mix_profile (integrated LUFS, true peak, crest, 9-band spectrum in dB, per-band stereo width). Streams via yt-dlp+ffmpeg in memory and never saves audio or extracts melodies/notes. `save_reference_as` writes the numbers to the reference profile file on disk — set it only when the person asked to keep this track as a reference. Requires yt-dlp and ffmpeg on PATH; you are responsible for your right to access the URL.",
		func(tc *ai.ToolContext, input AnalyzeAudioURLInput) (AnalyzeAudioURLOutput, error) {
			saveAs, err := checkReferenceName(store, input.SaveReferenceAs)
			if err != nil {
				return AnalyzeAudioURLOutput{}, err
			}
			got, err := audioanalyze.AnalyzeURL(tc, input.URL, analysisOptions(input.ProjectTempo, input.StartSec, input.EndSec))
			if err != nil {
				return AnalyzeAudioURLOutput{}, err
			}
			if saveAs != "" {
				if err := saveReference(store, saveAs, "url", got.Path, got); err != nil {
					return AnalyzeAudioURLOutput{}, err
				}
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
				RangeStartSec:     got.RangeStartSec,
				RangeEndSec:       got.RangeEndSec,
				MixProfile:        got.MixProfile,
				SavedReference:    saveAs,
				LengthBarsAtBPM:   got.LengthBarsAtBPM,
				Note:              got.Note,
				NextStep:          "Use match_axes + band_balance + section map as arrangement cues; build your own part from tempo/key/chords. This tool does not import audio — place only samples you have rights to use.",
			}, nil
		},
	)
}
