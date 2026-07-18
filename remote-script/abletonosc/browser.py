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

        self.osc_server.add_handler("/live/browser/find", browser_find_handler)
        self.osc_server.add_handler("/live/browser/debug", browser_debug_handler)
        self.osc_server.add_handler("/live/browser/list_folder", browser_list_folder_handler)
        self.osc_server.add_handler("/live/browser/load_at_path", browser_load_at_path_handler)
        self.osc_server.add_handler("/live/track/load/browser_item", track_load_browser_item_handler)
        self.osc_server.add_handler("/live/device/load/preset", device_load_preset_handler)
        self.osc_server.add_handler(
            "/live/clip_slot/create_audio_clip", clip_slot_create_audio_clip_handler
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
