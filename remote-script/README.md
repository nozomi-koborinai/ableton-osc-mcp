# AbletonOSC Browser + Master Patch

Adds Live Browser `load_item()` support and master-track mix helpers so MCP tools can load devices and adjust the mix bus without drag-and-drop.

Developed and smoke-tested against **Ableton Live 11.0.12** (API label `11.0`). Some handlers (notably `create_audio_clip`) require Live **12.0.5+**; see the main [README supported-versions section](../README.md#supported-ableton-live-versions).

## What it adds

| OSC address | Args | Purpose |
|-------------|------|---------|
| `/live/browser/find` | `query` | Search loadable items (up to 20 paths) |
| `/live/track/load/browser_item` | `track_index`, `item_name` | Load by name onto a track |
| `/live/device/load/preset` | `track_index`, `device_index`, `preset_name` | Hotswap preset onto a device |
| `/live/browser/list_folder` | _(none)_ or `root_name`, `*path_parts` | List roots, or children under a folder |
| `/live/browser/load_at_path` | `track_index` (`-1`=master), `device_index`, `root_name`, `*path_parts`, `item_name` | Load by exact path |
| `/live/browser/debug` | _(none)_ | Dump browser roots (debug) |
| `/live/clip_slot/create_audio_clip` | `track_index`, `clip_index`, `absolute_path` | Create a session audio clip from a local file (Live 12.0.5+) |
| `/live/device/get/parameters/value_string` | `track_index`, `device_index` | All parameter display strings in one reply (e.g. `37.0 Hz`, `1/2`, `Ins`) |
| `/live/device/set/parameter/string` | `track_index`, `device_index`, `parameter_index`, `value` | Set a parameter from a display string (enum name, or numeric with unit like `180 Hz`) |
| `/live/device/delete` | `track_index`, `device_index` | Delete a device; replies with the device name and count before/after (stock delete is silent) |
| `/live/device/get/is_active` | `track_index`, `device_index` | Device on/bypass state (`0`/`1`) for FX dry/wet A/B |
| `/live/device/set/is_active` | `track_index`, `device_index`, `is_active` | Set device on/bypass (`0`/`1`) |
| `/live/device/simpler/get` | `track_index`, `device_index` | Simpler state: playback_mode, slicing_playback_mode, slicing_style, slicing_beat_division, num_slices, has_sample |
| `/live/device/simpler/set` | `track_index`, `device_index`, `property`, `value` | Set a Simpler property (playback_mode / slicing_playback_mode / slicing_style / slicing_beat_division) |
| `/live/device/simpler/get/slices` | `track_index`, `device_index` | Slice map: sample_rate, sample_length, then slice start positions in samples |
| `/live/device/simpler/set/slices` | `track_index`, `device_index`, `*slice_samples` | Restore a manual slice map onto the same sample |
| `/live/clip/get/has_envelopes` | `track_index`, `clip_index` | Whether the Session clip has any envelopes |
| `/live/clip/envelope/get` | `track`, `clip`, `device_index` (`-1`=mixer), `param_index` [, `start`, `end`, `step`] | Sample envelope via `value_at_time` |
| `/live/clip/envelope/set_steps` | `track`, `clip`, `device_index`, `param_index`, `clear`, `*(time,duration,value)` | Create/write envelope steps |
| `/live/clip/envelope/clear` | `track`, `clip`, `device_index`, `param_index` | Clear one envelope |
| `/live/clip/envelope/clear_all` | `track`, `clip` | Clear all envelopes on the clip |
| `/live/song/get/return_tracks` | — | `(count, *names)` for return tracks (send indices) |
| `/live/device/get/available_input_routing_types` | `track_index`, `device_index` | Sidechain sources (Compressor on Live 11+) |
| `/live/device/get/available_input_routing_channels` | `track_index`, `device_index` | Sidechain channels for the current source |
| `/live/device/get/input_routing_type` | `track_index`, `device_index` | Current sidechain source |
| `/live/device/set/input_routing_type` | `track_index`, `device_index`, `type_name` | Set sidechain source by display name |
| `/live/device/get/input_routing_channel` | `track_index`, `device_index` | Current sidechain channel |
| `/live/device/set/input_routing_channel` | `track_index`, `device_index`, `channel_name` | Set sidechain channel by display name |
| `/live/master/get/volume` | — | Master volume |
| `/live/master/set/volume` | `volume` | Set master volume |
| `/live/master/get/output_meter_level` / `left` / `right` | — | Master meters |
| `/live/master/get/devices/name` / `class_name` / `type` | — | Master devices |
| `/live/master/device/get/parameters/{name,value,min,max}` | `device_index` | Master device params |
| `/live/master/device/set/parameter/value` | `device_index`, `parameter_index`, `value` | Set master device param |

## Install (macOS)

Assumes stock [AbletonOSC](https://github.com/ideoforms/AbletonOSC) is already at:

`~/Music/Ableton/User Library/Remote Scripts/AbletonOSC`

From this repository root:

```bash
ABLETONOSC="$HOME/Music/Ableton/User Library/Remote Scripts/AbletonOSC"

cp remote-script/abletonosc/browser.py "$ABLETONOSC/abletonosc/browser.py"
cp remote-script/abletonosc/master.py "$ABLETONOSC/abletonosc/master.py"
python3 remote-script/apply_manager_patch.py "$ABLETONOSC"
```

Then **fully restart Ableton Live** (hot-reload is not enough after `manager.py` changes).

## Manual manager.py edits

In `manager.py` `init_api` handlers list, add:

```python
abletonosc.BrowserHandler(self),
abletonosc.MasterHandler(self),
```

In `reload_imports`, add:

```python
importlib.reload(abletonosc.browser)
importlib.reload(abletonosc.master)
```

In `abletonosc/__init__.py`:

```python
from .browser import BrowserHandler
from .master import MasterHandler
```
