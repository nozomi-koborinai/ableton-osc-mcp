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
            """Params: root_name, *path_parts. Lists child names under a browser folder."""
            browser = Live.Application.get_application().browser
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
            Params: track_index, hotswap_device_index (-1 to append), root_name, *path_parts, item_name
            """
            track_index = int(params[0])
            device_index = int(params[1])
            if len(params) < 4:
                return ("error", "missing_path")
            root_name = str(params[2])
            *path_parts, item_name = [str(p) for p in params[3:]]
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

        self.osc_server.add_handler("/live/browser/find", browser_find_handler)
        self.osc_server.add_handler("/live/browser/debug", browser_debug_handler)
        self.osc_server.add_handler("/live/browser/list_folder", browser_list_folder_handler)
        self.osc_server.add_handler("/live/browser/load_at_path", browser_load_at_path_handler)
        self.osc_server.add_handler("/live/track/load/browser_item", track_load_browser_item_handler)
        self.osc_server.add_handler("/live/device/load/preset", device_load_preset_handler)
