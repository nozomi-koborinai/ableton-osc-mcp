package tools

import (
	"fmt"
	"reflect"
	"testing"
)

func describeCommands(cmds []auditionCommand) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		when := "on the line"
		if c.quantized {
			when = "ahead"
		}
		out = append(out, fmt.Sprintf("%s %v (%s)", c.address, c.args, when))
	}
	return out
}

func testBaseline() auditionState {
	return auditionState{
		clips:   map[int]int{2: 0, 5: -1},         // track 2 plays slot 0; track 5 is silent
		volumes: map[int]float64{2: 0.7, 3: 0.85}, // raw fader positions
		devices: map[[2]int]bool{{3, 1}: true},    // track 3, device 1 is on
	}
}

func TestStateForOverlaysOnlyWhatTheVariantNames(t *testing.T) {
	t.Parallel()

	b := auditionVariantState{Label: "B", clips: map[int]int{2: 1}, volumes: map[int]float64{3: 0.6}}
	got := stateFor(testBaseline(), b)

	want := auditionState{
		clips:   map[int]int{2: 1, 5: -1},
		volumes: map[int]float64{2: 0.7, 3: 0.6},
		devices: map[[2]int]bool{{3, 1}: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stateFor() = %+v, want %+v", got, want)
	}
	// The baseline itself must not be touched: every variant starts from it.
	if !reflect.DeepEqual(testBaseline(), auditionState{
		clips: map[int]int{2: 0, 5: -1}, volumes: map[int]float64{2: 0.7, 3: 0.85}, devices: map[[2]int]bool{{3, 1}: true},
	}) {
		t.Error("testBaseline changed")
	}
}

func TestCommandsBetweenSendsOnlyWhatDiffers(t *testing.T) {
	t.Parallel()

	base := testBaseline()
	b := stateFor(base, auditionVariantState{clips: map[int]int{2: 1}, volumes: map[int]float64{3: 0.6}})
	c := stateFor(base, auditionVariantState{clips: map[int]int{5: 2}, devices: map[[2]int]bool{{3, 1}: false}})

	// X -> B: one clip and one fader.
	if got, want := describeCommands(commandsBetween(base, b)), []string{
		"/live/clip_slot/fire [2 1] (ahead)",
		"/live/track/set/volume [3 0.6] (on the line)",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("X -> B = %v, want %v", got, want)
	}

	// B -> C: B's clip and fader go back to the baseline, C's own changes come in.
	if got, want := describeCommands(commandsBetween(b, c)), []string{
		"/live/clip_slot/fire [2 0] (ahead)",
		"/live/clip_slot/fire [5 2] (ahead)",
		"/live/track/set/volume [3 0.85] (on the line)",
		"/live/device/set/parameter/value [3 1 0 0] (on the line)",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("B -> C = %v, want %v", got, want)
	}

	// C -> X: track 5 was silent before the audition, so it is stopped rather than fired.
	if got, want := describeCommands(commandsBetween(c, base)), []string{
		"/live/track/stop_all_clips [5] (ahead)",
		"/live/device/set/parameter/value [3 1 0 1] (on the line)",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("C -> X = %v, want %v", got, want)
	}

	if got := commandsBetween(b, b); len(got) != 0 {
		t.Errorf("B -> B = %v, want nothing to send", describeCommands(got))
	}
}
