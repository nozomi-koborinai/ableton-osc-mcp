package tools

import (
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type SessionTrack struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

type SessionSnapshotOutput struct {
	TempoBPM  float64        `json:"tempo_bpm"`
	IsPlaying bool           `json:"is_playing"`
	NumScenes int            `json:"num_scenes"`
	Tracks    []SessionTrack `json:"tracks"`
}

type sessionQuerier interface {
	Query(address string, args ...interface{}) ([]interface{}, error)
}

func NewAbletonGetSessionSnapshot(g *genkit.Genkit, client *abletonosc.Client) ai.Tool {
	return genkit.DefineTool(g, "ableton_get_session_snapshot", "Ableton Live: get tempo, playback state, scenes, and indexed track names",
		func(_ *ai.ToolContext, _ EmptyInput) (SessionSnapshotOutput, error) {
			return getSessionSnapshot(client)
		},
	)
}

func getSessionSnapshot(client sessionQuerier) (SessionSnapshotOutput, error) {
	tempoRes, err := client.Query("/live/song/get/tempo")
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get tempo: %w", err)
	}
	if err := ensureResponseLen(tempoRes, 1); err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get tempo: %w", err)
	}
	tempo, err := abletonosc.AsFloat64(tempoRes[0])
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get tempo: %w", err)
	}

	playingRes, err := client.Query("/live/song/get/is_playing")
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get playback state: %w", err)
	}
	if err := ensureResponseLen(playingRes, 1); err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get playback state: %w", err)
	}
	isPlaying, err := abletonosc.AsBool(playingRes[0])
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get playback state: %w", err)
	}

	scenesRes, err := client.Query("/live/song/get/num_scenes")
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get scene count: %w", err)
	}
	if err := ensureResponseLen(scenesRes, 1); err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get scene count: %w", err)
	}
	numScenes, err := abletonosc.AsInt(scenesRes[0])
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get scene count: %w", err)
	}

	trackNamesRes, err := client.Query("/live/song/get/track_names")
	if err != nil {
		return SessionSnapshotOutput{}, fmt.Errorf("get track names: %w", err)
	}
	trackNames := toStringSlice(trackNamesRes)
	tracks := make([]SessionTrack, 0, len(trackNames))
	for index, name := range trackNames {
		tracks = append(tracks, SessionTrack{Index: index, Name: name})
	}

	return SessionSnapshotOutput{
		TempoBPM:  tempo,
		IsPlaying: isPlaying,
		NumScenes: numScenes,
		Tracks:    tracks,
	}, nil
}
