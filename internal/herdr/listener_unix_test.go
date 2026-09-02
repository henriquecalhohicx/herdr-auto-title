//go:build !windows

package herdr

import "net"

// listen opens the transport a test server accepts connections on: a Unix
// domain socket, matching what dial (client_unix.go) connects to.
func listen(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}
