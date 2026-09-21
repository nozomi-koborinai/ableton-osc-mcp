package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSongSections(t *testing.T) {
	t.Parallel()

	good := []SongSection{{SceneIndex: 2, Bars: 4, Name: "Intro"}, {SceneIndex: 0, Bars: 8}}
	if err := validateSongSections(good); err != nil {
		t.Fatalf("validateSongSections() error = %v", err)
	}

	many := make([]SongSection, 65)
	long := make([]SongSection, 7)
	for i := range many {
		many[i] = SongSection{Bars: 1}
	}
	for i := range long {
		long[i] = SongSection{Bars: 64} // 448 bars in all
	}
	for name, sections := range map[string][]SongSection{
		"none":           nil,
		"too many":       many,
		"too long":       long,
		"no bars":        {{SceneIndex: 0, Bars: 0}},
		"too many bars":  {{SceneIndex: 0, Bars: 65}},
		"negative scene": {{SceneIndex: -1, Bars: 4}},
		"long name":      {{SceneIndex: 0, Bars: 4, Name: strings.Repeat("あ", 41)}},
	} {
		err := validateSongSections(sections)
		var actionableErr *ActionableError
		if !errors.As(err, &actionableErr) || actionableErr.Code != "invalid_song_plan" {
			t.Errorf("%s: error = %v, want invalid_song_plan", name, err)
		}
	}
}

func TestResolveSongSectionsNamesThemAfterTheirScenes(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.sceneNames = []string{"Hook", "", "Intro"}
	got, err := resolveSongSections(live, []SongSection{{SceneIndex: 2, Bars: 4}, {SceneIndex: 0, Bars: 8, Name: "Hook 1"}, {SceneIndex: 1, Bars: 2}})
	if err != nil {
		t.Fatalf("resolveSongSections() error = %v", err)
	}
	if got[0].Name != "Intro" || got[1].Name != "Hook 1" || got[2].Name != "Section 3" {
		t.Errorf("names = %q %q %q; want the scene's name, the given name, and a numbered fallback", got[0].Name, got[1].Name, got[2].Name)
	}

	_, err = resolveSongSections(live, []SongSection{{SceneIndex: 7, Bars: 4}})
	var actionableErr *ActionableError
	if !errors.As(err, &actionableErr) || actionableErr.Code != "song_target_missing" {
		t.Errorf("scene 7 of 3: error = %v, want song_target_missing", err)
	}
}
