import re

import Live
from typing import Any, Optional, Tuple

from .handler import AbletonOSCHandler


def _browser_roots(browser):
    roots = []
    for attr in (
        "instruments",
        "sounds",
        "audio_effects",
        "midi_effects",
        "drums",
        "packs",
        "samples",
        "max_for_live",
        "plugins",
        "clips",
    ):
        try:
            roots.append(getattr(browser, attr))
        except Exception:
            pass
    try:
        for folder in browser.user_folders:
            roots.append(folder)
    except Exception:
        pass
    try:
        roots.append(browser.user_library)
    except Exception:
        pass
    return roots


def _find_browser_item(root, name: str) -> Optional[Any]:
    name_lower = name.lower()

    def walk(item):
        try:
            item_name = str(item.name)
            if item.is_loadable and (item_name == name or name_lower in item_name.lower()):
                return item
            if item.is_folder:
                for child in item.children:
                    found = walk(child)
                    if found is not None:
                        return found
        except Exception:
            pass
        return None

    return walk(root)


def _resolve_path(browser, root_name: str, path_parts):
    node = None
    for root in _browser_roots(browser):
        if str(root.name) == root_name:
            node = root
            break
    if node is None:
        return None
    for part in path_parts:
        node = next((c for c in node.children if str(c.name) == part), None)
        if node is None:
            return None
    return node


class BrowserHandler(AbletonOSCHandler):
    def __init__(self, manager):
        super().__init__(manager)
        self.class_identifier = "browser"

    def init_api(self):
        def browser_find_handler(params: Tuple[Any]):
            query_name = str(params[0])
            browser = Live.Application.get_application().browser
            matches = []

            def walk(item, path):
                try:
                    item_name = str(item.name)
                    current_path = f"{path}/{item_name}" if path else item_name
                    if item.is_loadable and query_name.lower() in item_name.lower():
                        matches.append(current_path)
                    if item.is_folder:
                        for child in item.children:
                            walk(child, current_path)
                except Exception:
                    pass

            for root in _browser_roots(browser):
                walk(root, "")
            return tuple(matches[:20])

        def browser_debug_handler(params: Tuple[Any]):
            browser = Live.Application.get_application().browser
            infos = []
            for root in _browser_roots(browser):
                try:
                    infos.append(str(root.name))
                    children = list(root.children)
                    infos.append(str(len(children)))
                    for child in children[:15]:
                        infos.append(str(child.name))
                except Exception as e:
                    infos.append("err:%s" % e)
            return tuple(infos[:40])

        def browser_list_folder_handler(params: Tuple[Any]):
            """Params: (none) lists browser roots; otherwise root_name, *path_parts."""
            browser = Live.Application.get_application().browser
            if not params:
                names = []
                for root in _browser_roots(browser):
                    try:
                        loadable = bool(getattr(root, "is_loadable", False))
                        names.append(
                            "%s|loadable=%s|folder=%s"
                            % (root.name, loadable, True)
                        )
                    except Exception:
                        names.append("%s|loadable=False|folder=True" % root.name)
                return ("roots", *names[:30])

            root_name = str(params[0])
            path_parts = [str(p) for p in params[1:]]
            node = _resolve_path(browser, root_name, path_parts)
            if node is None:
                if not path_parts:
                    return ("root_not_found", root_name)
                return ("path_not_found", root_name, *path_parts)
            names = []
            for child in node.children:
                try:
                    names.append(
                        "%s|loadable=%s|folder=%s"
                        % (child.name, child.is_loadable, child.is_folder)
                    )
                except Exception:
                    names.append(str(child.name))
            return (root_name, *path_parts, *names[:30])

        def browser_load_at_path_handler(params: Tuple[Any]):
            """Load browser item by path.
            Params: track_index (-1 = master), hotswap_device_index (-1 to append),
                    root_name, *path_parts, item_name
            """
            track_index = int(params[0])
            device_index = int(params[1])
            if len(params) < 4:
                return ("error", "missing_path")
            root_name = str(params[2])
            *path_parts, item_name = [str(p) for p in params[3:]]
            if track_index == -1:
                track = self.song.master_track
            else:
                track = self.song.tracks[track_index]
            browser = Live.Application.get_application().browser
            node = _resolve_path(browser, root_name, path_parts)
            if node is None:
                if not path_parts:
                    return (track_index, "root_not_found", root_name)
                return (track_index, "path_not_found", root_name, *path_parts, item_name)
            item = next(
                (
                    c
                    for c in node.children
                    if str(c.name) in (item_name, "%s.adv" % item_name, "%s.adg" % item_name)
                ),
                None,
            )
            if item is None:
                return (track_index, "item_not_found", item_name)
            devices_before = len(track.devices)
            self.song.view.selected_track = track
            if device_index >= 0:
                if device_index >= len(track.devices):
                    return (track_index, "invalid_device_index", device_index)
                self.song.view.hotswap_target = track.devices[device_index]
            else:
                self.song.view.hotswap_target = None
            browser.load_item(item)
            return (track_index, "loaded", item.name, devices_before, len(track.devices))

        def track_load_browser_item_handler(params: Tuple[Any]):
            """Params: track_index, item_name — search by name and append to track."""
            track_index = int(params[0])
            item_name = str(params[1])
            track = self.song.tracks[track_index]
            browser = Live.Application.get_application().browser
            item = None
            for root in _browser_roots(browser):
                item = _find_browser_item(root, item_name)
                if item is not None:
                    break
            if item is None:
                self.logger.warning("Browser item not found: %s" % item_name)
                return (track_index, "not_found", item_name)
            devices_before = len(track.devices)
            self.song.view.selected_track = track
            self.song.view.hotswap_target = None
            browser.load_item(item)
            self.logger.info("Loaded browser item '%s' on track %d" % (item.name, track_index))
            return (track_index, "loaded", item.name, devices_before, len(track.devices))

        def device_load_preset_handler(params: Tuple[Any]):
            """Params: track_index, device_index, preset_name — hotswap preset onto device."""
            track_index = int(params[0])
            device_index = int(params[1])
            preset_name = str(params[2])
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, "invalid_device_index", preset_name)
            device = track.devices[device_index]
            browser = Live.Application.get_application().browser
            item = None
            for root in _browser_roots(browser):
                item = _find_browser_item(root, preset_name)
                if item is not None:
                    break
            if item is None:
                self.logger.warning("Preset not found in browser: %s" % preset_name)
                return (track_index, device_index, "not_found", preset_name)
            self.song.view.selected_track = track
            self.song.view.hotswap_target = device
            browser.load_item(item)
            self.logger.info(
                "Loaded preset '%s' on track %d device %d" % (item.name, track_index, device_index)
            )
            return (track_index, device_index, "loaded", item.name)

        def clip_slot_create_audio_clip_handler(params: Tuple[Any]):
            """Params: track_index, clip_index, absolute_audio_path.

            Requires Live 12.0.5+ (ClipSlot.create_audio_clip). Used to drop
            local samples (e.g. synced Splice downloads) into session slots.
            """
            if len(params) < 3:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            path = str(params[2])
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, clip_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if track.has_midi_input:
                return (track_index, clip_index, "not_audio_track")
            if clip_index < 0 or clip_index >= len(track.clip_slots):
                return (track_index, clip_index, "invalid_clip_index")
            slot = track.clip_slots[clip_index]
            if not hasattr(slot, "create_audio_clip"):
                return (
                    track_index,
                    clip_index,
                    "unsupported",
                    "create_audio_clip requires Ableton Live 12.0.5+",
                )
            try:
                slot.create_audio_clip(path)
            except Exception as exc:
                return (track_index, clip_index, "error", str(exc))
            return (track_index, clip_index, "created", path)

        def device_get_parameters_value_string_handler(params: Tuple[Any]):
            """Params: track_index, device_index.

            Returns UI-friendly display strings for every parameter of a device
            (e.g. "37.0 Hz", "1/2", "Ins"). Stock AbletonOSC only exposes this per
            single parameter (/live/device/get/parameter/value_string), which is too
            many round-trips for devices with many parameters (e.g. EQ Eight has 84).
            Reply: (track_index, device_index, *display_strings) aligned with the
            /live/device/get/parameters/* lists.
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, device_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, "invalid_device_index")
            device = track.devices[device_index]
            strings = []
            for parameter in device.parameters:
                try:
                    strings.append(str(parameter.str_for_value(parameter.value)))
                except Exception:
                    strings.append("")
            return (track_index, device_index, *strings)

        def device_set_parameter_string_handler(params: Tuple[Any]):
            """Params: track_index, device_index, parameter_index, target_string.

            Sets a device parameter from a human-readable string. Three strategies:
            1. Enum params with value_items (e.g. Mix Type) -> match by name.
            2. Stepped params (e.g. Interval "1/2", Filter On "On") -> integer scan
               for an exact display match.
            3. Continuous params (e.g. "180 Hz", "-3.5 dB", "50 %") -> monotonic
               binary search over str_for_value, normalizing Hz/kHz.
            Reply: (track_index, device_index, parameter_index, status, value, display),
            or (..., "no_match", target, *options) when an enum value is not found.
            """
            if len(params) < 4:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            parameter_index = int(params[2])
            target = str(params[3]).strip()
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, device_index, parameter_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, parameter_index, "invalid_device_index")
            device = track.devices[device_index]
            if parameter_index < 0 or parameter_index >= len(device.parameters):
                return (track_index, device_index, parameter_index, "invalid_parameter_index")
            parameter = device.parameters[parameter_index]

            def display_of(value):
                try:
                    return str(parameter.str_for_value(value))
                except Exception:
                    return ""

            def numeric_in(text):
                text = str(text)
                match = re.search(r"-?\d+(?:\.\d+)?", text)
                if match is None:
                    return None
                value = float(match.group())
                lowered = text.lower()
                if "khz" in lowered:
                    value *= 1000.0
                return value

            # Strategy 1: enumerated parameter with named items.
            try:
                items = [str(x) for x in parameter.value_items]
            except Exception:
                items = []
            if items:
                target_lower = target.lower()
                chosen = None
                for i, item in enumerate(items):
                    if item == target:
                        chosen = i
                        break
                if chosen is None:
                    for i, item in enumerate(items):
                        if item.lower() == target_lower:
                            chosen = i
                            break
                if chosen is None and target_lower:
                    for i, item in enumerate(items):
                        if target_lower in item.lower():
                            chosen = i
                            break
                if chosen is None:
                    return (track_index, device_index, parameter_index, "no_match", target, *items)
                try:
                    parameter.value = float(parameter.min) + chosen
                except Exception as exc:
                    return (track_index, device_index, parameter_index, "error", str(exc))
                return (track_index, device_index, parameter_index, "set", parameter.value, display_of(parameter.value))

            # Strategy 2: stepped integer parameter matched by exact display string.
            steps = int(round(float(parameter.max) - float(parameter.min)))
            if 1 <= steps <= 512:
                target_lower = target.lower()
                for step in range(steps + 1):
                    candidate = float(parameter.min) + step
                    if display_of(candidate).lower() == target_lower:
                        try:
                            parameter.value = candidate
                        except Exception as exc:
                            return (track_index, device_index, parameter_index, "error", str(exc))
                        return (track_index, device_index, parameter_index, "set", parameter.value, display_of(parameter.value))

            # Strategy 3: continuous parameter resolved by monotonic binary search.
            target_num = numeric_in(target)
            if target_num is not None:
                lo = float(parameter.min)
                hi = float(parameter.max)
                lo_num = numeric_in(display_of(lo))
                hi_num = numeric_in(display_of(hi))
                if lo_num is not None and hi_num is not None and lo_num != hi_num:
                    increasing = hi_num >= lo_num
                    best = lo
                    for _ in range(48):
                        mid = (lo + hi) / 2.0
                        mid_num = numeric_in(display_of(mid))
                        if mid_num is None:
                            break
                        best = mid
                        if abs(mid_num - target_num) < 1e-6:
                            break
                        if (mid_num < target_num) == increasing:
                            lo = mid
                        else:
                            hi = mid
                    try:
                        parameter.value = best
                    except Exception as exc:
                        return (track_index, device_index, parameter_index, "error", str(exc))
                    return (track_index, device_index, parameter_index, "set", parameter.value, display_of(parameter.value))

            return (track_index, device_index, parameter_index, "no_match", target)

        def device_delete_handler(params: Tuple[Any]):
            """Params: track_index, device_index.

            Deletes a device and reports the device count before/after so the caller
            can confirm success. Stock /live/track/delete_device returns nothing,
            which makes success indistinguishable from a timeout.
            Reply: (track_index, device_index, status, device_name, before, after).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, device_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, "invalid_device_index")
            devices_before = len(track.devices)
            device_name = str(track.devices[device_index].name)
            try:
                track.delete_device(device_index)
            except Exception as exc:
                return (track_index, device_index, "error", str(exc))
            return (track_index, device_index, "deleted", device_name, devices_before, len(track.devices))

        def device_get_is_active_handler(params: Tuple[Any]):
            """Params: track_index, device_index.

            Reply: (track_index, device_index, is_active) where is_active is 0/1.
            Stock AbletonOSC does not expose Device.is_active for FX bypass A/B.
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, device_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, "invalid_device_index")
            device = track.devices[device_index]
            try:
                active = 1 if bool(device.is_active) else 0
            except Exception as exc:
                return (track_index, device_index, "error", str(exc))
            return (track_index, device_index, active)

        def device_set_is_active_handler(params: Tuple[Any]):
            """Params: track_index, device_index, is_active (0/1).

            Reply: (track_index, device_index, "ok", is_active).
            """
            if len(params) < 3:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            want = int(params[2]) != 0
            if track_index < 0 or track_index >= len(self.song.tracks):
                return (track_index, device_index, "invalid_track_index")
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return (track_index, device_index, "invalid_device_index")
            device = track.devices[device_index]
            try:
                device.is_active = want
                active = 1 if bool(device.is_active) else 0
            except Exception as exc:
                return (track_index, device_index, "error", str(exc))
            return (track_index, device_index, "ok", active)

        def _resolve_simpler(track_index: int, device_index: int):
            """Return (simpler_device, None) or (None, error_status)."""
            if track_index < 0 or track_index >= len(self.song.tracks):
                return None, "invalid_track_index"
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return None, "invalid_device_index"
            device = track.devices[device_index]
            # Simpler (class_name "OriginalSimpler") exposes playback_mode; other
            # devices do not, so this doubles as a type guard.
            if not hasattr(device, "playback_mode"):
                return None, "not_simpler"
            return device, None

        def device_simpler_get_handler(params: Tuple[Any]):
            """Params: track_index, device_index.

            Reply: (track, device, "ok", playback_mode, slicing_playback_mode,
                    slicing_style, slicing_beat_division, num_slices, has_sample).
            slicing_style / slicing_beat_division are -1 when no sample is loaded.
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_simpler(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            slicing_style = -1
            slicing_beat_division = -1
            num_slices = 0
            has_sample = 0
            try:
                sample = device.sample
            except Exception:
                sample = None
            if sample is not None:
                has_sample = 1
                try:
                    slicing_style = int(sample.slicing_style)
                except Exception:
                    pass
                try:
                    slicing_beat_division = int(sample.slicing_beat_division)
                except Exception:
                    pass
                try:
                    num_slices = len(sample.slices)
                except Exception:
                    pass
            return (
                track_index,
                device_index,
                "ok",
                int(device.playback_mode),
                int(device.slicing_playback_mode),
                slicing_style,
                slicing_beat_division,
                num_slices,
                has_sample,
            )

        def device_simpler_set_handler(params: Tuple[Any]):
            """Params: track_index, device_index, property, value (int).

            property in {playback_mode, slicing_playback_mode, slicing_style,
            slicing_beat_division}. Reply: (track, device, "set", property, value).
            """
            if len(params) < 4:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            prop = str(params[2])
            value = int(params[3])
            device, err = _resolve_simpler(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            try:
                if prop == "playback_mode":
                    device.playback_mode = value
                elif prop == "slicing_playback_mode":
                    device.slicing_playback_mode = value
                elif prop in ("slicing_style", "slicing_beat_division"):
                    sample = device.sample
                    if sample is None:
                        return (track_index, device_index, "no_sample", prop)
                    setattr(sample, prop, value)
                else:
                    return (track_index, device_index, "unknown_property", prop)
            except Exception as exc:
                return (track_index, device_index, "error", prop, str(exc))
            return (track_index, device_index, "set", prop, value)

        def device_simpler_get_slices_handler(params: Tuple[Any]):
            """Params: track_index, device_index.

            Reply: (track, device, "ok", sample_rate, sample_length, *slice_samples)
            where each slice value is a start position in samples; divide by
            sample_rate for seconds. Available since Live 11.
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_simpler(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            try:
                sample = device.sample
            except Exception:
                sample = None
            if sample is None:
                return (track_index, device_index, "no_sample")
            try:
                sample_rate = int(sample.sample_rate)
            except Exception:
                sample_rate = 0
            try:
                sample_length = int(sample.length)
            except Exception:
                sample_length = 0
            try:
                slices = [int(s) for s in sample.slices]
            except Exception:
                slices = []
            return (track_index, device_index, "ok", sample_rate, sample_length, *slices)

        def device_simpler_set_slices_handler(params: Tuple[Any]):
            """Params: track_index, device_index, *slice_samples.

            Replaces the Simpler's manual slices with the provided list (sample
            frames): switches slicing_style to Manual (3), clears existing
            slices, then inserts each. Used to restore a saved slice preset onto
            the same sample. Reply: (track, device, "ok", num_slices) or
            (track, device, err[, detail]).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            slice_samples = [int(p) for p in params[2:]]
            device, err = _resolve_simpler(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            try:
                sample = device.sample
            except Exception:
                sample = None
            if sample is None:
                return (track_index, device_index, "no_sample")
            try:
                sample.slicing_style = 3  # Manual
            except Exception:
                pass
            try:
                sample.clear_slices()
            except Exception as exc:
                return (track_index, device_index, "error", "clear_slices", str(exc))
            for value in slice_samples:
                try:
                    sample.insert_slice(int(value))
                except Exception as exc:
                    return (track_index, device_index, "error", "insert_slice", str(exc))
            try:
                num = len(sample.slices)
            except Exception:
                num = 0
            return (track_index, device_index, "ok", num)

        self.osc_server.add_handler("/live/browser/find", browser_find_handler)
        self.osc_server.add_handler("/live/browser/debug", browser_debug_handler)
        self.osc_server.add_handler("/live/browser/list_folder", browser_list_folder_handler)
        self.osc_server.add_handler("/live/browser/load_at_path", browser_load_at_path_handler)
        self.osc_server.add_handler("/live/track/load/browser_item", track_load_browser_item_handler)
        self.osc_server.add_handler("/live/device/load/preset", device_load_preset_handler)
        self.osc_server.add_handler(
            "/live/clip_slot/create_audio_clip", clip_slot_create_audio_clip_handler
        )
        self.osc_server.add_handler(
            "/live/device/get/parameters/value_string",
            device_get_parameters_value_string_handler,
        )
        self.osc_server.add_handler(
            "/live/device/set/parameter/string",
            device_set_parameter_string_handler,
        )
        self.osc_server.add_handler("/live/device/delete", device_delete_handler)
        self.osc_server.add_handler("/live/device/get/is_active", device_get_is_active_handler)
        self.osc_server.add_handler("/live/device/set/is_active", device_set_is_active_handler)
        self.osc_server.add_handler("/live/device/simpler/get", device_simpler_get_handler)
        self.osc_server.add_handler("/live/device/simpler/set", device_simpler_set_handler)
        self.osc_server.add_handler("/live/device/simpler/get/slices", device_simpler_get_slices_handler)
        self.osc_server.add_handler("/live/device/simpler/set/slices", device_simpler_set_slices_handler)

        #----------------------------------------------------------------------
        # Session clip automation envelopes (Live 11: insert_step / value_at_time)
        # device_index=-1 selects mixer: param 0=volume, 1=panning, 2+N=send N.
        #----------------------------------------------------------------------
        def _resolve_clip(track_index, clip_index):
            if track_index < 0 or track_index >= len(self.song.tracks):
                return None, None, "invalid_track_index"
            track = self.song.tracks[track_index]
            if clip_index < 0 or clip_index >= len(track.clip_slots):
                return None, None, "invalid_clip_index"
            clip = track.clip_slots[clip_index].clip
            if clip is None:
                return track, None, "no_clip"
            return track, clip, None

        def _resolve_envelope_param(track, device_index, parameter_index):
            if device_index == -1:
                mixer = track.mixer_device
                if parameter_index == 0:
                    return mixer.volume, None
                if parameter_index == 1:
                    return mixer.panning, None
                send_i = int(parameter_index) - 2
                if send_i < 0 or send_i >= len(mixer.sends):
                    return None, "invalid_send_index"
                return mixer.sends[send_i], None
            if device_index < 0 or device_index >= len(track.devices):
                return None, "invalid_device_index"
            device = track.devices[device_index]
            if parameter_index < 0 or parameter_index >= len(device.parameters):
                return None, "invalid_parameter_index"
            return device.parameters[parameter_index], None

        def clip_get_has_envelopes_handler(params: Tuple[Any]):
            """Params: track_index, clip_index. Reply: (t, c, has_envelopes)."""
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            _track, clip, err = _resolve_clip(track_index, clip_index)
            if err is not None:
                return (track_index, clip_index, err)
            try:
                has = bool(clip.has_envelopes)
            except Exception:
                has = False
            return (track_index, clip_index, has)

        def clip_envelope_get_handler(params: Tuple[Any]):
            """Params: track, clip, device_index, param_index [, start, end, step].
            Samples value_at_time across [start,end] (default 0..clip.length, step 0.25).
            Reply: (t, c, "ok"|"missing"|err, param_name, *time,value pairs)
            """
            if len(params) < 4:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            device_index = int(params[2])
            parameter_index = int(params[3])
            track, clip, err = _resolve_clip(track_index, clip_index)
            if err is not None:
                return (track_index, clip_index, err)
            param, perr = _resolve_envelope_param(track, device_index, parameter_index)
            if perr is not None:
                return (track_index, clip_index, perr)
            try:
                env = clip.automation_envelope(param)
            except Exception as exc:
                return (track_index, clip_index, "error", str(exc))
            pname = str(param.name)
            if env is None:
                return (track_index, clip_index, "missing", pname)
            try:
                length = float(clip.length)
            except Exception:
                length = 4.0
            start = float(params[4]) if len(params) > 4 else 0.0
            end = float(params[5]) if len(params) > 5 else length
            step = float(params[6]) if len(params) > 6 else 0.25
            if step <= 0:
                step = 0.25
            if end < start:
                start, end = end, start
            samples = []
            t = start
            # Cap samples to keep OSC replies bounded.
            max_points = 257
            while t <= end + 1e-9 and len(samples) < max_points * 2:
                try:
                    v = float(env.value_at_time(t))
                except Exception:
                    v = 0.0
                samples.append(t)
                samples.append(v)
                t += step
            return (track_index, clip_index, "ok", pname) + tuple(samples)

        def clip_envelope_set_steps_handler(params: Tuple[Any]):
            """Params: track, clip, device_index, param_index, clear_flag,
            then repeating (time, duration, value).
            clear_flag: 1 clears the envelope before inserting.
            Reply: (t, c, "ok", num_steps, param_name) or (t, c, err[, detail]).
            """
            if len(params) < 5:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            device_index = int(params[2])
            parameter_index = int(params[3])
            clear_flag = int(params[4])
            steps_raw = params[5:]
            if len(steps_raw) % 3 != 0:
                return (track_index, clip_index, "error", "steps_must_be_triples")
            track, clip, err = _resolve_clip(track_index, clip_index)
            if err is not None:
                return (track_index, clip_index, err)
            param, perr = _resolve_envelope_param(track, device_index, parameter_index)
            if perr is not None:
                return (track_index, clip_index, perr)
            pname = str(param.name)
            try:
                env = clip.automation_envelope(param)
            except Exception:
                env = None
            if env is None:
                try:
                    env = clip.create_automation_envelope(param)
                except Exception as exc:
                    return (track_index, clip_index, "error", "create", str(exc))
            if clear_flag:
                try:
                    clip.clear_envelope(param)
                    env = clip.create_automation_envelope(param)
                except Exception as exc:
                    return (track_index, clip_index, "error", "clear", str(exc))
            count = 0
            for i in range(0, len(steps_raw), 3):
                time_b = float(steps_raw[i])
                duration = float(steps_raw[i + 1])
                value = float(steps_raw[i + 2])
                try:
                    env.insert_step(time_b, duration, value)
                except Exception as exc:
                    return (track_index, clip_index, "error", "insert_step", str(exc), count)
                count += 1
            return (track_index, clip_index, "ok", count, pname)

        def clip_envelope_clear_handler(params: Tuple[Any]):
            """Params: track, clip, device_index, param_index.
            Reply: (t, c, "cleared"|"missing"|err, param_name?).
            """
            if len(params) < 4:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            device_index = int(params[2])
            parameter_index = int(params[3])
            track, clip, err = _resolve_clip(track_index, clip_index)
            if err is not None:
                return (track_index, clip_index, err)
            param, perr = _resolve_envelope_param(track, device_index, parameter_index)
            if perr is not None:
                return (track_index, clip_index, perr)
            pname = str(param.name)
            try:
                env = clip.automation_envelope(param)
            except Exception:
                env = None
            if env is None:
                return (track_index, clip_index, "missing", pname)
            try:
                clip.clear_envelope(param)
            except Exception as exc:
                return (track_index, clip_index, "error", str(exc))
            return (track_index, clip_index, "cleared", pname)

        def clip_envelope_clear_all_handler(params: Tuple[Any]):
            """Params: track, clip. Reply: (t, c, "cleared"|"noop"|err)."""
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            clip_index = int(params[1])
            _track, clip, err = _resolve_clip(track_index, clip_index)
            if err is not None:
                return (track_index, clip_index, err)
            try:
                has = bool(clip.has_envelopes)
            except Exception:
                has = False
            if not has:
                return (track_index, clip_index, "noop")
            try:
                clip.clear_all_envelopes()
            except Exception as exc:
                return (track_index, clip_index, "error", str(exc))
            return (track_index, clip_index, "cleared")

        self.osc_server.add_handler("/live/clip/get/has_envelopes", clip_get_has_envelopes_handler)
        self.osc_server.add_handler("/live/clip/envelope/get", clip_envelope_get_handler)
        self.osc_server.add_handler("/live/clip/envelope/set_steps", clip_envelope_set_steps_handler)
        self.osc_server.add_handler("/live/clip/envelope/clear", clip_envelope_clear_handler)
        self.osc_server.add_handler("/live/clip/envelope/clear_all", clip_envelope_clear_all_handler)

        #----------------------------------------------------------------------
        # Return tracks + device sidechain routing (Live 11+ CompressorDevice)
        #----------------------------------------------------------------------
        def song_get_return_tracks_handler(_params: Tuple[Any]):
            """Reply: (count, *names) for song.return_tracks."""
            returns = self.song.return_tracks
            return (len(returns),) + tuple(str(t.name) for t in returns)

        def _resolve_routable_device(track_index, device_index):
            if track_index < 0 or track_index >= len(self.song.tracks):
                return None, "invalid_track_index"
            track = self.song.tracks[track_index]
            if device_index < 0 or device_index >= len(track.devices):
                return None, "invalid_device_index"
            device = track.devices[device_index]
            if not hasattr(device, "available_input_routing_types"):
                return None, "unsupported"
            return device, None

        def device_get_available_input_routing_types_handler(params: Tuple[Any]):
            """Params: track_index, device_index.
            Reply: (track, device, "ok", *type_names) or (track, device, err).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            names = [str(t.display_name) for t in device.available_input_routing_types]
            return (track_index, device_index, "ok") + tuple(names)

        def device_get_available_input_routing_channels_handler(params: Tuple[Any]):
            """Params: track_index, device_index.
            Reply: (track, device, "ok", *channel_names) or (track, device, err).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            if not hasattr(device, "available_input_routing_channels"):
                return (track_index, device_index, "unsupported")
            names = [str(c.display_name) for c in device.available_input_routing_channels]
            return (track_index, device_index, "ok") + tuple(names)

        def device_get_input_routing_type_handler(params: Tuple[Any]):
            """Params: track_index, device_index.
            Reply: (track, device, "ok", type_name) or (track, device, err).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            try:
                name = str(device.input_routing_type.display_name)
            except Exception as exc:
                return (track_index, device_index, "error", str(exc))
            return (track_index, device_index, "ok", name)

        def device_set_input_routing_type_handler(params: Tuple[Any]):
            """Params: track_index, device_index, type_name.
            Reply: (track, device, "set", type_name) or (track, device, "not_found", type_name, *options)
            or (track, device, err).
            """
            if len(params) < 3:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            type_name = str(params[2])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            for routing_type in device.available_input_routing_types:
                if str(routing_type.display_name) == type_name:
                    try:
                        device.input_routing_type = routing_type
                    except Exception as exc:
                        return (track_index, device_index, "error", str(exc))
                    return (track_index, device_index, "set", type_name)
            options = [str(t.display_name) for t in device.available_input_routing_types]
            return (track_index, device_index, "not_found", type_name) + tuple(options)

        def device_get_input_routing_channel_handler(params: Tuple[Any]):
            """Params: track_index, device_index.
            Reply: (track, device, "ok", channel_name) or (track, device, err).
            """
            if len(params) < 2:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            if not hasattr(device, "input_routing_channel"):
                return (track_index, device_index, "unsupported")
            try:
                name = str(device.input_routing_channel.display_name)
            except Exception as exc:
                return (track_index, device_index, "error", str(exc))
            return (track_index, device_index, "ok", name)

        def device_set_input_routing_channel_handler(params: Tuple[Any]):
            """Params: track_index, device_index, channel_name.
            Reply: (track, device, "set", channel_name) or (track, device, "not_found", …).
            """
            if len(params) < 3:
                return ("error", "missing_args")
            track_index = int(params[0])
            device_index = int(params[1])
            channel_name = str(params[2])
            device, err = _resolve_routable_device(track_index, device_index)
            if err is not None:
                return (track_index, device_index, err)
            if not hasattr(device, "available_input_routing_channels"):
                return (track_index, device_index, "unsupported")
            for channel in device.available_input_routing_channels:
                if str(channel.display_name) == channel_name:
                    try:
                        device.input_routing_channel = channel
                    except Exception as exc:
                        return (track_index, device_index, "error", str(exc))
                    return (track_index, device_index, "set", channel_name)
            options = [str(c.display_name) for c in device.available_input_routing_channels]
            return (track_index, device_index, "not_found", channel_name) + tuple(options)

        self.osc_server.add_handler("/live/song/get/return_tracks", song_get_return_tracks_handler)
        self.osc_server.add_handler(
            "/live/device/get/available_input_routing_types",
            device_get_available_input_routing_types_handler,
        )
        self.osc_server.add_handler(
            "/live/device/get/available_input_routing_channels",
            device_get_available_input_routing_channels_handler,
        )
        self.osc_server.add_handler(
            "/live/device/get/input_routing_type",
            device_get_input_routing_type_handler,
        )
        self.osc_server.add_handler(
            "/live/device/set/input_routing_type",
            device_set_input_routing_type_handler,
        )
        self.osc_server.add_handler(
            "/live/device/get/input_routing_channel",
            device_get_input_routing_channel_handler,
        )
        self.osc_server.add_handler(
            "/live/device/set/input_routing_channel",
            device_set_input_routing_channel_handler,
        )

        # Master-track helpers (also registered by MasterHandler; kept here so hot-reload
        # of browser.py always restores them even if master.py fails to import).
        def _master():
            return self.song.master_track

        def master_get_volume(_params):
            return (_master().mixer_device.volume.value,)

        def master_set_volume(params):
            _master().mixer_device.volume.value = float(params[0])

        def master_get_meter_level(_params):
            return (_master().output_meter_level,)

        def master_get_meter_left(_params):
            return (_master().output_meter_left,)

        def master_get_meter_right(_params):
            return (_master().output_meter_right,)

        def master_get_num_devices(_params):
            return (len(_master().devices),)

        def master_get_devices_name(_params):
            return tuple(d.name for d in _master().devices)

        def master_get_devices_class_name(_params):
            return tuple(d.class_name for d in _master().devices)

        def master_get_devices_type(_params):
            return tuple(int(d.type) for d in _master().devices)

        def master_get_device_parameters_name(params):
            device_index = int(params[0])
            device = _master().devices[device_index]
            names = [p.name for p in device.parameters]
            return tuple([device_index] + names)

        def master_get_device_parameters_value(params):
            device_index = int(params[0])
            device = _master().devices[device_index]
            values = [p.value for p in device.parameters]
            return tuple([device_index] + values)

        def master_get_device_parameters_min(params):
            device_index = int(params[0])
            device = _master().devices[device_index]
            values = [p.min for p in device.parameters]
            return tuple([device_index] + values)

        def master_get_device_parameters_max(params):
            device_index = int(params[0])
            device = _master().devices[device_index]
            values = [p.max for p in device.parameters]
            return tuple([device_index] + values)

        def master_set_device_parameter(params):
            device_index = int(params[0])
            parameter_index = int(params[1])
            value = float(params[2])
            _master().devices[device_index].parameters[parameter_index].value = value
            return (device_index, parameter_index, value)

        self.osc_server.add_handler("/live/master/get/volume", master_get_volume)
        self.osc_server.add_handler("/live/master/set/volume", master_set_volume)
        self.osc_server.add_handler("/live/master/get/output_meter_level", master_get_meter_level)
        self.osc_server.add_handler("/live/master/get/output_meter_left", master_get_meter_left)
        self.osc_server.add_handler("/live/master/get/output_meter_right", master_get_meter_right)
        self.osc_server.add_handler("/live/master/get/num_devices", master_get_num_devices)
        self.osc_server.add_handler("/live/master/get/devices/name", master_get_devices_name)
        self.osc_server.add_handler("/live/master/get/devices/class_name", master_get_devices_class_name)
        self.osc_server.add_handler("/live/master/get/devices/type", master_get_devices_type)
        self.osc_server.add_handler("/live/master/device/get/parameters/name", master_get_device_parameters_name)
        self.osc_server.add_handler("/live/master/device/get/parameters/value", master_get_device_parameters_value)
        self.osc_server.add_handler("/live/master/device/get/parameters/min", master_get_device_parameters_min)
        self.osc_server.add_handler("/live/master/device/get/parameters/max", master_get_device_parameters_max)
        self.osc_server.add_handler("/live/master/device/set/parameter/value", master_set_device_parameter)
