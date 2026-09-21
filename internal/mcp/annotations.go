package mcp

import "github.com/mark3labs/mcp-go/mcp"

// hints describes how a tool behaves, for MCP tool annotations.
// readOnly wins over destructive: a read-only tool is never destructive.
type hints struct {
	readOnly    bool
	destructive bool
	idempotent  bool
}

func boolPtr(b bool) *bool { return &b }

// annotationsFor returns the MCP annotations for a tool name.
// Unknown names fall back to the most cautious hints.
func annotationsFor(name string) mcp.ToolAnnotation {
	h, ok := toolHints[name]
	if !ok {
		h = hints{destructive: true}
	}
	return mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(h.readOnly),
		DestructiveHint: boolPtr(!h.readOnly && h.destructive),
		IdempotentHint:  boolPtr(h.idempotent),
		// Every tool talks to Ableton Live or the local filesystem.
		OpenWorldHint: boolPtr(true),
	}
}

// toolHints classifies every registered tool. TestEveryToolIsClassified fails
// when a new tool is added without an entry here.
//
// readOnly     - changes neither the Live session nor local files
// destructive  - removes or replaces something that already exists
// idempotent   - repeating the same call leaves the same state
var toolHints = map[string]hints{
	"ableton_clip_read":  {readOnly: true},
	"ableton_clip_write": {destructive: true, idempotent: true},
	// The analyze tools read audio, but save_reference_as writes the reference
	// profile file, so they cannot claim readOnly for the calls that only read.
	"ableton_analyze_audio_url":              {idempotent: true},
	"ableton_analyze_local_audio":            {idempotent: true},
	"ableton_apply_device_intent":            {idempotent: true},
	"ableton_apply_mix_variation":            {idempotent: true},
	"ableton_arm_track":                      {idempotent: true},
	"ableton_audition_ab":                    {},
	"ableton_autogain_tracks":                {idempotent: true},
	"ableton_bounce_session_pass":            {},
	"ableton_capture_mix_snapshot":           {readOnly: true},
	"ableton_chop_draft":                     {readOnly: true},
	"ableton_clear_clip_envelope":            {destructive: true, idempotent: true},
	"ableton_compare_ab_variation":           {},
	"ableton_compare_fx_bypass":              {},
	"ableton_create_audio_track":             {},
	"ableton_create_midi_track":              {},
	"ableton_create_named_scenes":            {},
	"ableton_create_return_track":            {},
	"ableton_create_scene_energy_variation":  {},
	"ableton_delete_clip":                    {destructive: true, idempotent: true},
	"ableton_delete_device":                  {destructive: true, idempotent: true},
	"ableton_delete_track":                   {destructive: true, idempotent: true},
	"ableton_diagnose":                       {},
	"ableton_duplicate_clip_to":              {},
	"ableton_duplicate_track":                {},
	"ableton_duplicate_track_for_processing": {},
	"ableton_extract_clip_region":            {},
	"ableton_find_browser_item":              {readOnly: true},
	"ableton_fire_clip_slot":                 {},
	"ableton_fire_scene":                     {},
	"ableton_get_clip_envelope":              {readOnly: true},
	"ableton_get_clip_properties":            {readOnly: true},
	"ableton_get_device_parameters":          {readOnly: true},
	"ableton_get_device_sidechain":           {readOnly: true},
	"ableton_get_master_device_parameters":   {readOnly: true},
	"ableton_get_master_devices":             {readOnly: true},
	"ableton_get_master_meter":               {readOnly: true},
	"ableton_get_master_volume":              {readOnly: true},
	"ableton_get_return_tracks":              {readOnly: true},
	"ableton_get_scene_names":                {readOnly: true},
	"ableton_get_session_record":             {readOnly: true},
	"ableton_get_session_snapshot":           {readOnly: true},
	"ableton_get_simpler":                    {readOnly: true},
	"ableton_get_simpler_slices":             {readOnly: true},
	"ableton_get_sounding_snapshot":          {readOnly: true},
	"ableton_get_splice_library":             {readOnly: true},
	"ableton_get_taste_profile":              {readOnly: true},
	"ableton_get_tempo":                      {readOnly: true},
	"ableton_get_track_devices":              {readOnly: true},
	"ableton_get_track_input_routing":        {readOnly: true},
	"ableton_get_track_meter":                {readOnly: true},
	"ableton_get_track_names":                {readOnly: true},
	"ableton_get_track_sends":                {readOnly: true},
	"ableton_list_browser_folder":            {readOnly: true},
	"ableton_list_intents":                   {readOnly: true},
	"ableton_list_reference_profiles":        {readOnly: true},
	"ableton_list_slice_presets":             {readOnly: true},
	"ableton_load_browser_item":              {},
	"ableton_load_browser_path":              {},
	"ableton_load_device_preset":             {},
	"ableton_load_on_master":                 {},
	"ableton_load_slice_preset":              {idempotent: true},
	"ableton_load_splice_sample":             {},
	"ableton_match_clip_tempo":               {},
	"ableton_measure_mix":                    {},
	"ableton_mute_track":                     {idempotent: true},
	"ableton_osc_send":                       {destructive: true, idempotent: true},
	"ableton_play":                           {idempotent: true},
	"ableton_preview_destructive":            {readOnly: true},
	"ableton_record_variation_preference":    {idempotent: true},
	"ableton_restore_mix_snapshot":           {idempotent: true},
	"ableton_save_slice_preset":              {idempotent: true},
	"ableton_search_splice_samples":          {readOnly: true},
	"ableton_set_clip_envelope_steps":        {destructive: true},
	"ableton_set_clip_pitch":                 {idempotent: true},
	"ableton_set_clip_region":                {idempotent: true},
	"ableton_set_clip_warp":                  {idempotent: true},
	"ableton_set_device_parameter":           {idempotent: true},
	"ableton_set_device_parameter_string":    {idempotent: true},
	"ableton_set_device_sidechain":           {idempotent: true},
	"ableton_set_master_device_parameter":    {idempotent: true},
	"ableton_set_master_volume":              {idempotent: true},
	"ableton_set_metronome":                  {idempotent: true},
	"ableton_set_monitoring":                 {idempotent: true},
	"ableton_set_scene_clip_presence":        {destructive: true, idempotent: true},
	"ableton_set_scene_name":                 {idempotent: true},
	"ableton_set_session_record":             {idempotent: true},
	"ableton_set_simpler_playback_mode":      {idempotent: true},
	"ableton_set_simpler_slicing":            {idempotent: true},
	"ableton_set_song_key":                   {idempotent: true},
	"ableton_set_tempo":                      {idempotent: true},
	"ableton_set_track_input_routing":        {idempotent: true},
	"ableton_set_track_name":                 {idempotent: true},
	"ableton_set_track_send":                 {idempotent: true},
	"ableton_set_track_volume":               {idempotent: true},
	"ableton_setup_drum_track":               {},
	"ableton_solo_track":                     {idempotent: true},
	"ableton_stop":                           {idempotent: true},
	"ableton_stop_all_clips":                 {idempotent: true},
	"ableton_stop_clip":                      {idempotent: true},
	"ableton_test":                           {readOnly: true},
}
