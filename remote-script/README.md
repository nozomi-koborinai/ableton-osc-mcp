# AbletonOSC Browser Patch

Adds Live Browser `load_item()` support so MCP tools can load Drum Racks and presets without drag-and-drop.

## What it adds

| OSC address | Args | Purpose |
|-------------|------|---------|
| `/live/browser/find` | `query` | Search loadable items (up to 20 paths) |
| `/live/track/load/browser_item` | `track_index`, `item_name` | Load by name onto a track |
| `/live/device/load/preset` | `track_index`, `device_index`, `preset_name` | Hotswap preset onto a device |
| `/live/browser/list_folder` | `root_name`, `*path_parts` | List folder children |
| `/live/browser/load_at_path` | `track_index`, `device_index`, `root_name`, `*path_parts`, `item_name` | Load by exact path |
| `/live/browser/debug` | _(none)_ | Dump browser roots (debug) |

## Install (macOS)

Assumes stock [AbletonOSC](https://github.com/ideoforms/AbletonOSC) is already at:

`~/Music/Ableton/User Library/Remote Scripts/AbletonOSC`

From this repository root:

```bash
ABLETONOSC="$HOME/Music/Ableton/User Library/Remote Scripts/AbletonOSC"

# 1. Copy BrowserHandler
cp remote-script/abletonosc/browser.py "$ABLETONOSC/abletonosc/browser.py"

# 2. Register the handler (idempotent-ish — skip if already present)
grep -q 'BrowserHandler' "$ABLETONOSC/abletonosc/__init__.py" || \
  printf '\nfrom .browser import BrowserHandler\n' >> "$ABLETONOSC/abletonosc/__init__.py"

# 3. Add BrowserHandler to manager.py handlers list and reload_imports
#    See "Manual manager.py edits" below if you prefer editing by hand.
python3 remote-script/apply_manager_patch.py "$ABLETONOSC"
```

Then in Ableton Live:

1. Preferences → Link / Tempo / MIDI → Control Surface = **AbletonOSC**
2. **Restart Ableton Live** (required the first time you apply this patch — `/live/api/reload` does not reload `manager.py`)

After a full restart, `/live/browser/find` should respond.

## Manual manager.py edits

In `manager.py` `init_api` handlers list, add:

```python
abletonosc.BrowserHandler(self),
```

In `reload_imports`, add:

```python
importlib.reload(abletonosc.browser)
```

## Cleanup (optional)

If you previously patched browser logic into `device.py` / `track.py`, remove those inline handlers so only `browser.py` owns them. Stock AbletonOSC does not include them, so a fresh clone + this patch is enough.

## Windows paths

```text
%USERPROFILE%\Documents\Ableton\User Library\Remote Scripts\AbletonOSC
```
