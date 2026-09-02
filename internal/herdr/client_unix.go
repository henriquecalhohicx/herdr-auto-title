//go:build !windows

package herdr

import (
	"context"
	"net"
)

// dial connects to the Herdr socket at path, a Unix domain socket on every
// platform but Windows; see client_windows.go for the named-pipe equivalent.
func dial(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer

	return d.DialContext(ctx, "unix", path)
}
