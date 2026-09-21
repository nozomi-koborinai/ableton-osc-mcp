package tools

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/nozomi-koborinai/ableton-osc-mcp/internal/abletonosc"
)

type mixerDBClient interface {
	Send(address string, args ...interface{}) error
	Query(address string, args ...interface{}) ([]interface{}, error)
}

// mixerTarget names one mixer level: a track's volume, one of its sends, or
// the master volume.
type mixerTarget struct {
	kind  string // "track", "send" or "master"
	track int
	send  int
}

func trackVolumeTarget(track int) mixerTarget { return mixerTarget{kind: "track", track: track} }

func trackSendTarget(track, send int) mixerTarget {
	return mixerTarget{kind: "send", track: track, send: send}
}

func masterVolumeTarget() mixerTarget { return mixerTarget{kind: "master"} }

// indexArgs are the leading arguments every address for this target takes;
// replies echo them back before the payload.
func (t mixerTarget) indexArgs() []interface{} {
	switch t.kind {
	case "track":
		return []interface{}{int32(t.track)}
	case "send":
		return []interface{}{int32(t.track), int32(t.send)}
	default:
		return nil
	}
}

func (t mixerTarget) levelAddress() string {
	switch t.kind {
	case "track":
		return "/live/track/get/volume_db"
	case "send":
		return "/live/track/get/send_db"
	default:
		return "/live/master/get/volume_db"
	}
}

func (t mixerTarget) resolveAddress() string {
	switch t.kind {
	case "track":
		return "/live/track/get/volume_for_db"
	case "send":
		return "/live/track/get/send_for_db"
	default:
		return "/live/master/get/volume_for_db"
	}
}

func (t mixerTarget) setAddress() string {
	switch t.kind {
	case "track":
		return "/live/track/set/volume"
	case "send":
		return "/live/track/set/send"
	default:
		return "/live/master/set/volume"
	}
}

// mixerLevel is one level as Live shows it.
type mixerLevel struct {
	Raw     float64
	Display string // empty when the dB handlers are not installed
	DB      float64
	Silent  bool // the display reads -inf dB
}

// levelChange says how a level should change: exactly one of a raw position,
// an absolute dB value, or a dB delta from the current level.
type levelChange struct {
	Raw     *float64
	DB      *float64
	DeltaDB *float64
}

var dbNumber = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// parseDBDisplay reads a mixer display string such as "-6.0 dB" or "-inf dB".
func parseDBDisplay(display string) (db float64, silent bool, err error) {
	lowered := strings.ToLower(strings.TrimSpace(display))
	if strings.HasPrefix(lowered, "-inf") {
		return 0, true, nil
	}
	number := dbNumber.FindString(lowered)
	if number == "" || !strings.Contains(lowered, "db") {
		return 0, false, fmt.Errorf("not a dB display string: %q", display)
	}
	db, err = strconv.ParseFloat(number, 64)
	return db, false, err
}

var errMixerDBPatchMissing = &ActionableError{
	Code:     "mixer_db_patch_missing",
	Message:  "this AbletonOSC install has no dB mixer handlers",
	NextStep: "Copy remote-script/abletonosc/browser.py and master.py into AbletonOSC again, then restart Live (or send /live/api/reload). Raw values (0.0-1.0) keep working meanwhile.",
}

func isMixerDBPatchMissing(err error) bool {
	return errors.Is(err, errMixerDBPatchMissing)
}

func mixerQueryError(err error) error {
	if err != nil && strings.Contains(err.Error(), "no response received to query") {
		return errMixerDBPatchMissing
	}
	return err
}

// payloadAfterIndexEcho drops the echoed index arguments. Error replies may
// echo fewer of them, so it stops at the first value that is not an integer.
func payloadAfterIndexEcho(res []interface{}, indexCount int) []interface{} {
	i := 0
	for i < indexCount && i < len(res) {
		switch res[i].(type) {
		case int32, int64, int:
			i++
			continue
		}
		break
	}
	return res[i:]
}

func mixerStatusError(payload []interface{}) error {
	status := "unexpected_reply"
	if len(payload) > 0 {
		status = fmt.Sprint(payload[len(payload)-1])
	}
	switch status {
	case "invalid_track_index":
		return actionable(status, "no track at that index", "Call ableton_get_track_names and pick a valid track_index.")
	case "invalid_send_index":
		return actionable(status, "the track has no send at that index", "Call ableton_get_return_tracks; send_index matches return track order (0=A, 1=B, …).")
	default:
		return fmt.Errorf("unexpected mixer reply: %v", payload)
	}
}

// queryMixerLevel reads a level the way Live displays it.
func queryMixerLevel(c oscQuerier, t mixerTarget) (mixerLevel, error) {
	res, err := c.Query(t.levelAddress(), t.indexArgs()...)
	if err != nil {
		return mixerLevel{}, mixerQueryError(err)
	}
	payload := payloadAfterIndexEcho(res, len(t.indexArgs()))
	if len(payload) < 2 {
		return mixerLevel{}, mixerStatusError(payload)
	}
	return levelFromReply(payload[0], payload[1])
}

func levelFromReply(display, raw interface{}) (mixerLevel, error) {
	rawValue, err := abletonosc.AsFloat64(raw)
	if err != nil {
		return mixerLevel{}, err
	}
	text := fmt.Sprint(display)
	db, silent, err := parseDBDisplay(text)
	if err != nil {
		return mixerLevel{}, err
	}
	return mixerLevel{Raw: rawValue, Display: text, DB: db, Silent: silent}, nil
}

// resolveRawForDB asks Live which raw position displays as db. Nothing changes.
func resolveRawForDB(c oscQuerier, t mixerTarget, db float64) (float64, error) {
	args := append(t.indexArgs(), float32(db))
	res, err := c.Query(t.resolveAddress(), args...)
	if err != nil {
		return 0, mixerQueryError(err)
	}
	payload := payloadAfterIndexEcho(res, len(t.indexArgs()))
	if len(payload) < 3 {
		return 0, mixerStatusError(payload)
	}
	raw, err := abletonosc.AsFloat64(payload[0])
	if err != nil {
		return 0, err
	}
	if fmt.Sprint(payload[2]) != "ok" {
		return 0, actionable("level_out_of_range",
			fmt.Sprintf("%.1f dB is outside what this fader can show; it stops at %v", db, payload[1]),
			"Ask for a level within the fader's range.")
	}
	return raw, nil
}

// applyLevelChange validates the request, resolves dB to a raw position when
// needed, sets it with the stock setter, and reads the level back. rawField
// names the raw input ("volume" or "value") for error messages.
func applyLevelChange(c mixerDBClient, t mixerTarget, change levelChange, rawField string) (mixerLevel, error) {
	given := 0
	for _, v := range []*float64{change.Raw, change.DB, change.DeltaDB} {
		if v != nil {
			given++
			if math.IsNaN(*v) || math.IsInf(*v, 0) {
				return mixerLevel{}, invalidLevelChange(rawField, "values must be finite numbers")
			}
		}
	}
	if given != 1 {
		return mixerLevel{}, invalidLevelChange(rawField, fmt.Sprintf("got %d of them", given))
	}

	var raw float64
	switch {
	case change.Raw != nil:
		if *change.Raw < 0 || *change.Raw > 1 {
			return mixerLevel{}, invalidLevelChange(rawField, rawField+" must be 0.0 to 1.0")
		}
		raw = *change.Raw
	case change.DB != nil:
		resolved, err := resolveRawForDB(c, t, *change.DB)
		if err != nil {
			return mixerLevel{}, err
		}
		raw = resolved
	default:
		current, err := queryMixerLevel(c, t)
		if err != nil {
			return mixerLevel{}, err
		}
		if current.Silent {
			return mixerLevel{}, actionable("delta_from_silence",
				"the level is at -inf dB, so a dB change has nothing to start from",
				"Set an absolute level with db instead.")
		}
		resolved, err := resolveRawForDB(c, t, current.DB+*change.DeltaDB)
		if err != nil {
			return mixerLevel{}, err
		}
		raw = resolved
	}

	if err := c.Send(t.setAddress(), append(t.indexArgs(), float32(raw))...); err != nil {
		return mixerLevel{}, err
	}
	level, err := queryMixerLevel(c, t)
	if isMixerDBPatchMissing(err) {
		return mixerLevel{Raw: raw}, nil // set by raw value on an old patch: no display to report
	}
	return level, err
}

func invalidLevelChange(rawField, detail string) error {
	return actionable("invalid_level_change",
		fmt.Sprintf("give exactly one of %s, db, delta_db (%s)", rawField, detail),
		fmt.Sprintf("Use db for an absolute level (e.g. -6), delta_db for a change (e.g. -2), or %s for a raw position 0.0-1.0.", rawField))
}

// queryTrackVolumesDB reads every track's volume in one reply, in track order.
func queryTrackVolumesDB(c oscQuerier) ([]mixerLevel, error) {
	res, err := c.Query("/live/song/get/track_volumes_db")
	if err != nil {
		return nil, mixerQueryError(err)
	}
	if len(res) < 1 {
		return nil, fmt.Errorf("unexpected mixer reply: %v", res)
	}
	count, err := abletonosc.AsInt(res[0])
	if err != nil || len(res) < 1+2*count {
		return nil, fmt.Errorf("unexpected mixer reply: %v", res)
	}
	levels := make([]mixerLevel, 0, count)
	for i := 0; i < count; i++ {
		level, err := levelFromReply(res[1+2*i], res[2+2*i])
		if err != nil {
			return nil, err
		}
		levels = append(levels, level)
	}
	return levels, nil
}
