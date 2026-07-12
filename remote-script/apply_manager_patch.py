#!/usr/bin/env python3
"""Patch AbletonOSC manager.py to register BrowserHandler."""

from __future__ import annotations

import sys
from pathlib import Path


def patch(manager_path: Path) -> None:
    text = manager_path.read_text(encoding="utf-8")
    original = text

    if "abletonosc.BrowserHandler(self)" not in text:
        text = text.replace(
            "abletonosc.DeviceHandler(self),",
            "abletonosc.DeviceHandler(self),\n                abletonosc.BrowserHandler(self),",
            1,
        )

    if "abletonosc.MasterHandler(self)" not in text:
        # Prefer registering after BrowserHandler when present.
        if "abletonosc.BrowserHandler(self)," in text:
            text = text.replace(
                "abletonosc.BrowserHandler(self),",
                "abletonosc.BrowserHandler(self),\n                abletonosc.MasterHandler(self),",
                1,
            )
        else:
            text = text.replace(
                "abletonosc.DeviceHandler(self),",
                "abletonosc.DeviceHandler(self),\n                abletonosc.MasterHandler(self),",
                1,
            )

    if "importlib.reload(abletonosc.browser)" not in text:
        text = text.replace(
            "importlib.reload(abletonosc.device)",
            "importlib.reload(abletonosc.device)\n            importlib.reload(abletonosc.browser)",
            1,
        )

    if "importlib.reload(abletonosc.master)" not in text:
        anchor = "importlib.reload(abletonosc.browser)"
        if anchor in text:
            text = text.replace(
                anchor,
                anchor + "\n            importlib.reload(abletonosc.master)",
                1,
            )
        else:
            text = text.replace(
                "importlib.reload(abletonosc.device)",
                "importlib.reload(abletonosc.device)\n            importlib.reload(abletonosc.master)",
                1,
            )

    if text == original:
        print("manager.py already patched (or pattern not found)")
        return

    manager_path.write_text(text, encoding="utf-8")
    print(f"Patched {manager_path}")


def ensure_init_import(init_path: Path) -> None:
    text = init_path.read_text(encoding="utf-8")
    changed = False
    if "from .browser import BrowserHandler" not in text:
        if not text.endswith("\n"):
            text += "\n"
        text += "from .browser import BrowserHandler\n"
        changed = True
    if "from .master import MasterHandler" not in text:
        if not text.endswith("\n"):
            text += "\n"
        text += "from .master import MasterHandler\n"
        changed = True
    if not changed:
        print("__init__.py already imports BrowserHandler/MasterHandler")
        return
    init_path.write_text(text, encoding="utf-8")
    print(f"Updated {init_path}")


def main() -> int:
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} /path/to/AbletonOSC", file=sys.stderr)
        return 2
    root = Path(sys.argv[1]).expanduser().resolve()
    manager = root / "manager.py"
    init_py = root / "abletonosc" / "__init__.py"
    if not manager.is_file() or not init_py.is_file():
        print(f"Not an AbletonOSC install: {root}", file=sys.stderr)
        return 1
    ensure_init_import(init_py)
    patch(manager)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
