# AbletonOSC Browser + Master Patch

Adds Live Browser `load_item()` support and master-track mix helpers so MCP tools can load devices and adjust the mix bus without drag-and-drop.

## What it adds

| OSC address | Args | Purpose |
|-------------|------|---------|
| `/live/browser/find` | `query` | Search loadable items (up to 20 paths) |
| `/live/track/load/browser_item` | `track_index`, `item_name` | Load by name onto a track |
| `/live/device/load/preset` | `track_index`, `device_index`, `preset_name` | Hotswap preset onto a device |
| `/live/browser/list_folder` | _(none)_ or `root_name`, `*path_parts` | List roots, or children under a folder |
| `/live/browser/load_at_path` | `track_index` (`-1`=master), `device_index`, `root_name`, `*path_parts`, `item_name` | Load by exact path |
| `/live/browser/debug` | _(none)_ | Dump browser roots (debug) |
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
