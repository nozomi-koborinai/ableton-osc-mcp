package tools

import (
	"errors"
	"math"
	"testing"
	"time"
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

// slowPollLive is a transport whose every answer takes a tenth of a second, the
// way AbletonOSC answers on its timer.
type slowPollLive struct {
	songTime float64
	tempo    float64
}

func (l *slowPollLive) Send(string, ...interface{}) error { return nil }

func (l *slowPollLive) Query(address string, _ ...interface{}) ([]interface{}, error) {
	l.songTime += 0.1 * l.tempo / 60
	switch address {
	case "/live/song/get/current_song_time":
		return []interface{}{float32(l.songTime)}, nil
	case "/live/song/get/is_playing":
		return []interface{}{int32(1)}, nil
	}
	return nil, errors.New("unexpected query: " + address)
}

func TestWaitUntilSongTimeDoesNotOvershootByAPollRoundTrip(t *testing.T) {
	t.Parallel()

	for _, target := range []float64{7.7, 8, 9.13} {
		live := &slowPollLive{tempo: 120}
		sleep := func(d time.Duration) { live.songTime += d.Seconds() * live.tempo / 60 }
		if err := waitUntilSongTime(live, sleep, target, live.tempo); err != nil {
			t.Fatalf("waitUntilSongTime() error = %v", err)
		}
		// A fader sent 150 ms ahead of a bar line must not arrive after it: the
		// wait may be a few milliseconds late, not a whole round trip (0.2 beats here).
		if late := live.songTime - target; late < -1e-6 || late > 0.02 {
			t.Errorf("target %v: returned at beat %.3f, %.3f beats late", target, live.songTime, late)
		}
	}
}
