#!/bin/sh
# Rebuild and restart Auto Title whenever a source file changes.
#
# Run this in a tab of its own and leave it in the foreground: Ctrl+C stops both
# the watcher and the plugin. Backgrounding it is how you end up with a stray
# process quietly renaming tabs.
set -eu

BINARY=./herdr-auto-title
PKG=./cmd/herdr-auto-title
PID=""

case "$(uname)" in
Darwin) mtime() { stat -f %m "$1"; } ;;
*) mtime() { stat -c %Y "$1"; } ;;
esac

# A cheap fingerprint of every source file's modification time.
fingerprint() {
	{
		find . -type f -name '*.go' -not -path './.git/*' -print
		echo ./go.mod
	} | sort | while read -r file; do
		[ -f "$file" ] && mtime "$file"
	done | cksum
}

stop_plugin() {
	if [ -n "$PID" ]; then
		kill "$PID" 2>/dev/null || true
		wait "$PID" 2>/dev/null || true
		PID=""
	fi
}

trap 'echo; echo "--- stopping"; stop_plugin; exit 0' INT TERM

echo "--- watching for changes; Ctrl+C to stop"
last=""
while true; do
	current=$(fingerprint)
	if [ "$current" != "$last" ]; then
		last=$current
		stop_plugin
		if go build -o "$BINARY" "$PKG"; then
			echo "--- $(date '+%H:%M:%S') started"
			HERDR_AUTO_TITLE_DEBUG=1 "$BINARY" &
			PID=$!
		else
			echo "--- $(date '+%H:%M:%S') build failed; nothing is running"
		fi
	fi
	sleep 1
done
