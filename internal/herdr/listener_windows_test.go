//go:build windows

package herdr

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// listen opens the transport a test server accepts connections on: a named
// pipe, matching what dial (client_windows.go) connects to.
func listen(path string) (net.Listener, error) {
	return winio.ListenPipe(pipeName(path), nil)
}
