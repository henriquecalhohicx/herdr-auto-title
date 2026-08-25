#!/usr/bin/env python3
"""Rebuild and restart Auto Title whenever a source file changes.

Run this in a tab of its own and leave it in the foreground: Ctrl+C stops both
the watcher and the plugin. Backgrounding it is how you end up with a stray
process quietly renaming tabs.
"""

import os
import pathlib
import signal
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent
BINARY = ROOT / "herdr-auto-title"
PACKAGE = "./cmd/herdr-auto-title"

# How often the sources are looked at. Long enough to be free, short enough
# that a save is followed by a restart before you have switched tabs.
INTERVAL = 1.0

# How long a plugin gets to shut down before it is killed outright.
GRACE = 5.0


def sources():
    """Every file a rebuild depends on."""
    yield ROOT / "go.mod"
    for path in ROOT.rglob("*.go"):
        if ".git" not in path.parts:
            yield path


def fingerprint():
    """A cheap summary of when the sources were last touched."""
    stamps = []
    for path in sorted(sources()):
        try:
            stamps.append((str(path), path.stat().st_mtime_ns))
        except OSError:
            # The file went away between being listed and being read.
            pass
    return tuple(stamps)


def build():
    """Compile the plugin, reporting whether the binary is now fresh."""
    build = subprocess.run(["go", "build", "-o", str(BINARY), PACKAGE], cwd=ROOT)
    return build.returncode == 0


def start():
    """Run the plugin with DEBUG logging, in this process's own terminal."""
    env = {**os.environ, "HERDR_AUTO_TITLE_DEBUG": "1"}
    return subprocess.Popen([str(BINARY)], cwd=ROOT, env=env)


def stop(plugin):
    """Stop the plugin and wait for it, so no instance outlives the watcher."""
    if plugin is None:
        return
    plugin.terminate()
    try:
        plugin.wait(timeout=GRACE)
    except subprocess.TimeoutExpired:
        plugin.kill()
        plugin.wait()


def clock():
    return time.strftime("%H:%M:%S")


def watch():
    print("--- watching for changes; Ctrl+C to stop")
    plugin = None
    last = None
    try:
        while True:
            current = fingerprint()
            if current != last:
                last = current
                stop(plugin)
                plugin = None
                if build():
                    plugin = start()
                    print(f"--- {clock()} started")
                else:
                    print(f"--- {clock()} build failed; nothing is running")
            elif plugin is not None and plugin.poll() is not None:
                # A plugin that stopped on its own would otherwise be missed
                # until the next edit, and the tabs would quietly stop moving.
                code = plugin.returncode
                print(f"--- {clock()} plugin exited ({code}); waiting for a change")
                plugin = None
            time.sleep(INTERVAL)
    finally:
        print()
        print("--- stopping")
        stop(plugin)


def interrupt(signum, frame):
    """Turn a signal into the interrupt Ctrl+C already raises.

    `make stop` sends SIGTERM, and Python would end the process on the spot.
    Raising here instead runs the cleanup, so the plugin is never orphaned.
    """
    raise KeyboardInterrupt


def main():
    signal.signal(signal.SIGTERM, interrupt)
    try:
        watch()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
