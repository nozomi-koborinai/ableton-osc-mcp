# ableton-osc-mcp

![hero-image](./assets/hero.jpeg)

An MCP (Model Context Protocol) server for controlling **Ableton Live 11+** via [AbletonOSC](https://github.com/ideoforms/AbletonOSC).

This enables AI assistants (Claude, Cursor, etc.) to interact with Ableton Live for beat-making, music production, and creative workflows.

## Features

- Control Ableton Live from AI assistants via MCP
- Create MIDI tracks and clips
- Add, read, and clear MIDI notes
- Get/set tempo
- Inspect tempo, playback state, scenes, and indexed tracks in one snapshot
- List devices on a track
- Load Drum Racks / presets from Live's Browser (with the included AbletonOSC patch)
- Browse Live Browser folders by path and load items onto tracks
- Set up a drum track with kit + clip + pattern in one recipe
- Humanize MIDI clips with microtiming, velocity variation, and swing
- Autogain tracks toward a target meter level while audio is playing
- Diagnose AbletonOSC connection and browser/master patch readiness
- Fire clip slots
- Send raw OSC messages for advanced control

## Browser loading (AbletonOSC patch)

Stock [AbletonOSC](https://github.com/ideoforms/AbletonOSC) does not expose Live's Browser `load_item()` API.
This repo ships a small Remote Script patch under [`remote-script/`](remote-script/) that adds:

- `/live/browser/find`
- `/live/browser/list_folder`
- `/live/browser/load_at_path`
- `/live/track/load/browser_item`
- `/live/device/load/preset`

Install steps: see [remote-script/README.md](remote-script/README.md).
After applying the patch, **restart Ableton Live** (a full restart is required the first time; `/live/api/reload` alone is not enough).

## How it Works

```mermaid
flowchart LR
    subgraph AI["AI Assistant"]
        Claude["Claude / Cursor"]
    end

    subgraph MCP["MCP Server"]
        Server["ableton-osc-mcp<br/>(Go + Genkit)"]
    end

    subgraph Ableton["Ableton Live"]
        OSC["AbletonOSC<br/>(Python Remote Script)"]
        Live["Live Object Model"]
    end

    Claude <-->|"MCP (stdio)<br/>JSON-RPC"| Server
    Server <-->|"OSC / UDP<br/>port 11000-11001"| OSC
    OSC <--> Live
```

### What is OSC?

[OSC (Open Sound Control)](https://opensoundcontrol.stanford.edu/) is a network protocol designed for real-time communication between music software and hardware. It uses UDP for low-latency messaging, making it ideal for music applications where speed matters more than guaranteed delivery.

### Communication Flow

1. **AI Assistant → ableton-osc-mcp**: MCP protocol over stdio (JSON-RPC)
2. **ableton-osc-mcp → AbletonOSC**: OSC messages over UDP (port 11000)
3. **AbletonOSC → ableton-osc-mcp**: OSC responses over UDP (port 11001)
4. **AbletonOSC → Ableton Live**: Direct access to Live Object Model (internal API)

The MCP server acts as a **translator** between MCP tool calls and OSC messages.

### Comparison with ableton-mcp

There's another Ableton MCP implementation: [ableton-mcp](https://github.com/ahujasid/ableton-mcp). Here's how they differ:

| | ableton-osc-mcp (this project) | ableton-mcp |
|---|---|---|
| **Remote Script** | [AbletonOSC](https://github.com/ideoforms/AbletonOSC) (existing OSS) | Custom implementation |
| **Protocol** | OSC / UDP (standard) | JSON over TCP sockets |
| **Language** | Go | Python |
| **Approach** | Uses standard OSC protocol | Custom protocol |

From an MCP client's perspective, both may **feel similar** (tools that create clips, set tempo, etc.). The big difference is the **underlying transport + remote script** (AbletonOSC/OSC vs custom script/socket protocol).

**Which should you choose?**

- **ableton-osc-mcp**: If you prefer standard protocols or want to reuse AbletonOSC for other purposes
- **ableton-mcp**: If you want tighter integration or need features not available via OSC

Both work well — choose based on your preference.

## Prerequisites

- **Ableton Live 11** or later
- **AbletonOSC** installed and enabled in Live

## Installation

> **Note**: Ableton Live is officially supported on **macOS** and **Windows** only.

### 1. Install AbletonOSC

> **Tip**: If the clone directory already exists, delete it first or skip the `git clone` step.

#### macOS

```bash
git clone https://github.com/ideoforms/AbletonOSC.git /tmp/AbletonOSC
mkdir -p ~/Music/Ableton/User\ Library/Remote\ Scripts
cp -r /tmp/AbletonOSC ~/Music/Ableton/User\ Library/Remote\ Scripts/AbletonOSC
```

#### Windows (PowerShell)

```powershell
git clone https://github.com/ideoforms/AbletonOSC.git $env:TEMP\AbletonOSC
Copy-Item -Recurse $env:TEMP\AbletonOSC "$env:USERPROFILE\Documents\Ableton\User Library\Remote Scripts\AbletonOSC"
```

#### Windows (Command Prompt)

```cmd
git clone https://github.com/ideoforms/AbletonOSC.git %TEMP%\AbletonOSC
xcopy /E /I %TEMP%\AbletonOSC "%USERPROFILE%\Documents\Ableton\User Library\Remote Scripts\AbletonOSC"
```

### 2. Enable AbletonOSC in Ableton Live

1. Open Ableton Live
2. Go to **Preferences** → **Link / Tempo / MIDI**
3. Under **Control Surface**, select **AbletonOSC**
4. Restart Ableton Live

<img src="assets/preferences.png" alt="AbletonOSC Control Surface settings" width="450">

### 3. Install ableton-osc-mcp

Choose the installation method that best fits your environment:

#### Option A: Homebrew (macOS/Linux) — Recommended

```bash
brew tap nozomi-koborinai/tap
brew install ableton-osc-mcp
```

The binary will be installed to `/opt/homebrew/bin/ableton-osc-mcp` (Apple Silicon) or `/usr/local/bin/ableton-osc-mcp` (Intel/Linux).

#### Option B: Download pre-built binary

Download from [GitHub Releases](https://github.com/nozomi-koborinai/ableton-osc-mcp/releases) for your platform:

| Binary | Platform | Architecture |
|--------|----------|--------------|
| `ableton-osc-mcp-darwin-arm64` | macOS | Apple Silicon |
| `ableton-osc-mcp-darwin-amd64` | macOS | Intel |
| `ableton-osc-mcp-linux-amd64` | Linux | x86_64 |
| `ableton-osc-mcp-windows-amd64.exe` | Windows | x86_64 |

> **macOS users**: After downloading, remove the quarantine attribute:
>
> ```bash
> chmod +x ableton-osc-mcp-darwin-*
> xattr -d com.apple.quarantine ableton-osc-mcp-darwin-*
> ```

#### Option C: Build from source

Requires Go 1.25+:

```bash
git clone https://github.com/nozomi-koborinai/ableton-osc-mcp.git
cd ableton-osc-mcp
go build -o ableton-osc-mcp .
```

### 4. Configure MCP Client

Find your binary path first:

```bash
# If installed via Homebrew
which ableton-osc-mcp
# Output: /opt/homebrew/bin/ableton-osc-mcp (Apple Silicon)
#         /usr/local/bin/ableton-osc-mcp (Intel/Linux)
```

#### Cursor

Add to `.cursor/mcp.json` in your project or global config:

```json
{
  "mcpServers": {
    "ableton-osc-mcp": {
      "command": "/opt/homebrew/bin/ableton-osc-mcp"
    }
  }
}
```

#### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "ableton-osc-mcp": {
      "command": "/opt/homebrew/bin/ableton-osc-mcp"
    }
  }
}
```

> **Note**: Replace `/opt/homebrew/bin/ableton-osc-mcp` with your actual binary path if different.

## Available Tools

| Tool | Description |
|------|-------------|
| `ableton_test` | Test connection to AbletonOSC |
| `ableton_diagnose` | Diagnose AbletonOSC connection and browser/master patch availability |
| `ableton_get_tempo` / `ableton_set_tempo` | Get/set tempo (BPM) |
| `ableton_play` / `ableton_stop` / `ableton_stop_all_clips` | Transport |
| `ableton_set_song_key` | Set root note and scale |
| `ableton_set_metronome` | Enable/disable metronome |
| `ableton_get_session_snapshot` | Get tempo, playback state, scene count, and indexed track names |
| `ableton_get_track_names` | List track names |
| `ableton_get_track_devices` | List devices on a track |
| `ableton_create_midi_track` | Create a MIDI track |
| `ableton_create_audio_track` | Create an audio track |
| `ableton_set_track_name` | Rename a track |
| `ableton_mute_track` / `ableton_solo_track` | Mute/solo |
| `ableton_arm_track` | Arm/disarm for recording |
| `ableton_get_track_input_routing` / `ableton_set_track_input_routing` | Input routing (e.g. Resampling) |
| `ableton_set_monitoring` | Monitoring state (0=In 1=Auto 2=Off) |
| `ableton_set_track_volume` | Set track volume |
| `ableton_create_clip` | Create a clip in a slot |
| `ableton_get_clip_notes` / `ableton_add_midi_notes` / `ableton_clear_clip_notes` | MIDI notes |
| `ableton_humanize_clip` | Add microtiming, velocity variation, and optional swing to clip notes |
| `ableton_fire_clip_slot` / `ableton_stop_clip` | Fire/stop a clip |
| `ableton_duplicate_clip_to` | Duplicate clip to another slot |
| `ableton_set_clip_name` | Rename a clip |
| `ableton_fire_scene` | Fire a scene |
| `ableton_get_device_parameters` / `ableton_set_device_parameter` | Device parameters |
| `ableton_find_browser_item` | Search Live Browser (requires patch) |
| `ableton_list_browser_folder` | List Browser roots or folder children (requires patch) |
| `ableton_load_browser_item` | Load Drum Rack / instrument onto a track by name |
| `ableton_load_browser_path` | Load Browser item onto a track by exact path (requires patch) |
| `ableton_load_device_preset` | Hotswap a preset onto a device |
| `ableton_get_track_meter` | Track output meter levels |
| `ableton_autogain_tracks` | Iteratively adjust track volumes toward a target meter level |
| `ableton_get_master_meter` / `ableton_get_master_volume` / `ableton_set_master_volume` | Master meter/volume (requires master patch) |
| `ableton_get_master_devices` / `ableton_get_master_device_parameters` / `ableton_set_master_device_parameter` | Master devices (requires master patch) |
| `ableton_load_on_master` | Load Browser item onto master (requires browser+master patch) |
| `ableton_get_session_record` / `ableton_set_session_record` | Session Record on/off |
| `ableton_bounce_session_pass` | Record a scene pass onto a Bounce track via Resampling (tens of seconds; does not export WAV) |
| `ableton_setup_drum_track` | Create MIDI drum track, load kit, fill clip with preset pattern (requires browser patch) |
| `ableton_osc_send` | Send raw OSC message |

## Example Usage

Once configured, you can ask your AI assistant:

- "Set the tempo to 140 BPM"
- "Create a MIDI track, load Street Kit, and add a 4-bar clip"
- "Set up a Street Kit drum track with a four-on-floor pattern"
- "Humanize the drum clip with a bit of swing"
- "Autogain the drum and bass tracks while the beat is playing"
- "Find drum kits named Street in the browser"
- "List the Drums browser folder, then load Street Kit onto track 0"
- "Add a kick drum pattern on beats 1, 2, 3, 4"
- "What's the current tempo?"
- "Diagnose the AbletonOSC connection and patches"

## Built With

- [Go](https://go.dev/) - Programming language
- [Genkit for Go](https://genkit.dev/docs/model-context-protocol/?lang=go) - AI framework with MCP support
- [AbletonOSC](https://github.com/ideoforms/AbletonOSC) - OSC interface for Ableton Live

## License

MIT License - see [LICENSE](LICENSE) for details.

## Related Projects

- [AbletonOSC](https://github.com/ideoforms/AbletonOSC) - OSC interface for Ableton Live (used by this project)
- [ableton-mcp](https://github.com/ahujasid/ableton-mcp) - Alternative MCP implementation (see [comparison](#comparison-with-ableton-mcp))

## Advanced Configuration

<details>
<summary>Environment Variables (usually not needed)</summary>

In most cases, the default settings work fine. Change these only if:

- **Ableton Live is running on a different machine** → change `ABLETON_OSC_HOST`
- **Port conflicts with other software** → change port settings
- **Heavy projects cause timeout errors** → increase `ABLETON_OSC_TIMEOUT_MS`

| Variable | Default | Description |
|----------|---------|-------------|
| `ABLETON_OSC_HOST` | `127.0.0.1` | AbletonOSC host |
| `ABLETON_OSC_PORT` | `11000` | AbletonOSC listen port |
| `ABLETON_OSC_CLIENT_PORT` | `11001` | Port for receiving replies |
| `ABLETON_OSC_TIMEOUT_MS` | `500` | Query timeout in milliseconds |

</details>

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
