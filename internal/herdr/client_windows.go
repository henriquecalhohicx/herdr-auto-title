//go:build windows

package herdr

import (
	"context"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
)

// pipePrefixes are the two forms HERDR_SOCKET_PATH already arrives in when it
// names a pipe outright, mirroring herdr-cache-ttl's pipe_name_str.
var pipePrefixes = []string{`\\.\pipe\`, `\\?\pipe\`}

// dial connects to the Herdr named pipe backing HERDR_SOCKET_PATH. Herdr
// exposes a filesystem-style path on Windows, but the real endpoint is the
// named pipe `\\.\pipe\` plus that path, not the path itself.
func dial(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName(path))
}

func pipeName(path string) string {
	for _, prefix := range pipePrefixes {
		if strings.HasPrefix(path, prefix) {
			return path
		}
	}

	return `\\.\pipe\` + path
}
