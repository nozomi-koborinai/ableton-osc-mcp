package tools

import (
	"math"
	"testing"
)

func TestBarLeadBeatsIsHalfASecondButAtLeastABeat(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tempo       float64
		beatsPerBar int
		want        float64
	}{
		{60, 4, 1},    // half a second is half a beat: a whole beat is the floor
		{120, 4, 1},   // half a second is exactly a beat
		{180, 4, 1.5}, // half a second is a beat and a half
		{480, 4, 3.5}, // never the whole bar
	} {
		if got := barLeadBeats(tc.tempo, tc.beatsPerBar); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("barLeadBeats(%v, %d) = %v, want %v", tc.tempo, tc.beatsPerBar, got, tc.want)
		}
	}
}

func TestNextSafeBarLineLetsACloseLineGoBy(t *testing.T) {
	t.Parallel()

	live := newFakeRecorder()
	live.songTime = 1.5 // two and a half beats before bar line 4: plenty of time
	_, line, err := nextSafeBarLine(live, live.advance, live.tempo, live.signature)
	if err != nil || line != 4 {
		t.Errorf("from beat 1.5: line = %v, %v; want 4", line, err)
	}

	live.songTime = 3.9 // a command sent now would land after bar line 4
	now, line, err := nextSafeBarLine(live, live.advance, live.tempo, live.signature)
	if err != nil || line != 8 || now < 4 {
		t.Errorf("from beat 3.9: now = %v line = %v, %v; want to be past 4 and aiming for 8", now, line, err)
	}
}
