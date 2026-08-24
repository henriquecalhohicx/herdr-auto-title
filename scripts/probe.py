#!/usr/bin/env python3
"""Inspect the live Herdr socket.

Auto Title depends on protocol details the specification got wrong, so verify
against the running Herdr before writing code that assumes anything.
"""

import json
import os
import socket
import subprocess
import sys
import time

USAGE = """usage: scripts/probe.py <command>

  tabs         one-shot list of tab ids and labels
  watch-tabs   the same, refreshed every second
  snapshot     the session snapshot Auto Title bootstraps from
  events       subscribe and print events as they arrive (Ctrl+C to stop)
  subs         subscription types this Herdr accepts, with their required fields
  version      protocol and version of the running Herdr
"""

# Subscriptions use dot notation; the events they deliver arrive snake_case.
PROBE_SUBSCRIPTIONS = [
    "tab.created",
    "tab.closed",
    "tab.renamed",
    "pane.created",
    "pane.closed",
    "pane.updated",
    "pane.focused",
    "pane.agent_detected",
]


def herdr(*args):
    """Run a herdr CLI command and return its parsed JSON result."""
    out = subprocess.run(
        ["herdr", *args], capture_output=True, text=True, check=True
    ).stdout
    payload = json.loads(out)
    if "error" in payload:
        raise SystemExit(f"herdr {' '.join(args)}: {payload['error']}")
    return payload["result"]


def socket_path():
    path = os.environ.get("HERDR_SOCKET_PATH")
    if not path:
        raise SystemExit(
            "HERDR_SOCKET_PATH is not set: run this from inside a Herdr pane"
        )
    return path


def cmd_tabs():
    for tab in herdr("tab", "list")["tabs"]:
        print(f"{tab['tab_id']:10} {tab['agent_status']:8} {tab['label']}")


def cmd_watch_tabs():
    while True:
        print("\033[2J\033[H", end="")
        print(time.strftime("%H:%M:%S"))
        cmd_tabs()
        sys.stdout.flush()
        time.sleep(1)


def cmd_snapshot():
    snap = herdr("api", "snapshot")["snapshot"]
    print(f"herdr {snap['version']}, protocol {snap['protocol']}")
    print("\ntabs")
    for tab in snap["tabs"]:
        print(f"  {tab['tab_id']:10} panes={tab['pane_count']} label={tab['label']!r}")
    print("\npanes")
    for pane in snap["panes"]:
        agent = pane.get("agent") or "-"
        print(f"  {pane['pane_id']:10} tab={pane['tab_id']:10} agent={agent:8} cwd={pane.get('cwd')}")
        title = pane.get("terminal_title_stripped")
        if title:
            print(f"  {'':10} title={title!r}")


def cmd_subs():
    schema = json.loads(
        subprocess.run(
            ["herdr", "api", "schema", "--json"],
            capture_output=True,
            text=True,
            check=True,
        ).stdout
    )
    variants = schema["schemas"]["request"]["$defs"]["Subscription"]["oneOf"]
    for variant in variants:
        props = variant.get("properties", {})
        name = props.get("type", {}).get("const")
        required = [
            key
            for key in props
            if key != "type" and key in variant.get("required", [])
        ]
        note = f"  (requires {', '.join(required)})" if required else ""
        print(f"  {name}{note}")


def describe(event):
    """One readable line per event."""
    data = event["data"]
    pane = data.get("pane")
    tab = data.get("tab")

    if pane:
        ident = pane["pane_id"]
        extra = f" cwd={pane.get('cwd')} title={pane.get('terminal_title_stripped')!r}"
    elif tab:
        ident = tab["tab_id"]
        extra = f" label={tab.get('label')!r}"
    else:
        ident = data.get("pane_id") or data.get("tab_id") or ""
        extra = f" label={data['label']!r}" if "label" in data else ""

    return f"{event['event']:28} {ident:10}{extra}"


def cmd_events():
    conn = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.connect(socket_path())
    request = {
        "id": "probe",
        "method": "events.subscribe",
        "params": {
            "subscriptions": [{"type": name} for name in PROBE_SUBSCRIPTIONS]
        },
    }
    conn.sendall((json.dumps(request) + "\n").encode())

    buf = b""
    while True:
        chunk = conn.recv(65536)
        if not chunk:
            print("stream closed by herdr", file=sys.stderr)
            return
        buf += chunk
        while b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            if not line.strip():
                continue
            frame = json.loads(line)
            if "event" not in frame:
                # The subscribe acknowledgement, or a rejection.
                print(f"-- {json.dumps(frame)}", flush=True)
                continue
            print(describe(frame), flush=True)


def cmd_version():
    snap = herdr("api", "snapshot")["snapshot"]
    print(f"herdr {snap['version']}, protocol {snap['protocol']}")


COMMANDS = {
    "tabs": cmd_tabs,
    "watch-tabs": cmd_watch_tabs,
    "snapshot": cmd_snapshot,
    "events": cmd_events,
    "subs": cmd_subs,
    "version": cmd_version,
}


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in COMMANDS:
        print(USAGE, file=sys.stderr)
        return 1
    try:
        COMMANDS[sys.argv[1]]()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
