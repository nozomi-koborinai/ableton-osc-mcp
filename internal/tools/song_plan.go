package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxSongSections    = 64
	maxSongSectionBars = 64
	maxSongBars        = 400
	maxSongSectionName = 40
)

// SongSection is one stretch of a song: a scene, for so many bars. A song is a
// list of them, passed in whole every time; nothing about it is kept here,
// where it could drift away from what is in the Live set.
type SongSection struct {
	SceneIndex int    `json:"scene_index" jsonschema:"description=Scene that plays during this section,minimum=0"`
	Bars       int    `json:"bars" jsonschema:"description=Length of the section in bars,minimum=1,maximum=64"`
	Name       string `json:"name,omitempty" jsonschema:"description=Section name\\, e.g. Intro or Hook 1 (default: the scene's name)"`
}

func invalidSongPlan(format string, args ...interface{}) error {
	return actionable("invalid_song_plan", fmt.Sprintf(format, args...),
		"Fix that and call again. Nothing in Live was touched.")
}

// validateSongSections checks what can be checked without Live.
func validateSongSections(sections []SongSection) error {
	if n := len(sections); n < 1 || n > maxSongSections {
		return invalidSongPlan("a song takes 1 to %d sections, got %d", maxSongSections, n)
	}
	total := 0
	for i, section := range sections {
		if section.SceneIndex < 0 {
			return invalidSongPlan("sections[%d]: scene_index must be 0 or more", i)
		}
		if section.Bars < 1 || section.Bars > maxSongSectionBars {
			return invalidSongPlan("sections[%d]: bars must be 1 to %d, got %d", i, maxSongSectionBars, section.Bars)
		}
		if utf8.RuneCountInString(strings.TrimSpace(section.Name)) > maxSongSectionName {
			return invalidSongPlan("sections[%d]: name must be %d characters or fewer", i, maxSongSectionName)
		}
		total += section.Bars
	}
	if total > maxSongBars {
		return invalidSongPlan("the song is %d bars long; %d is the most one pass takes", total, maxSongBars)
	}
	return nil
}

// resolveSongSections makes sure every scene exists and gives each section a
// name: its own, else its scene's, else its number. It only reads.
func resolveSongSections(client oscQuerier, sections []SongSection) ([]SongSection, error) {
	names, err := querySceneNames(client)
	if err != nil {
		return nil, fmt.Errorf("get scene names: %w", err)
	}
	out := make([]SongSection, len(sections))
	for i, section := range sections {
		if section.SceneIndex >= len(names) {
			return nil, actionable("song_target_missing",
				fmt.Sprintf("sections[%d] names scene %d, but the set has %d scenes", i, section.SceneIndex, len(names)),
				"Call ableton_get_scene_names and use the indices it lists. Nothing in Live was touched.")
		}
		section.Name = strings.TrimSpace(section.Name)
		if section.Name == "" {
			section.Name = strings.TrimSpace(names[section.SceneIndex])
		}
		if section.Name == "" {
			section.Name = fmt.Sprintf("Section %d", i+1)
		}
		out[i] = section
	}
	return out, nil
}
