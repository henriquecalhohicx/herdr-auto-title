package resolver

import (
	"strings"

	"github.com/kryptamine/herdr-auto-title/internal/state"
)

const (
	// sshKind marks a host as reached over ssh, bound to it the way any kind is
	// bound to its detail: `ssh › prod-01`.
	//
	// The mark belongs on the host rather than in the activity slot, because
	// the activity is contested: a remote shell sets a terminal title, that
	// title outranks anything this source could put there, and the tab would
	// stop saying it is remote at exactly the moment it has most to say. The
	// host slot has no such competition — nothing else names a machine.
	sshKind = "ssh"
	// SSHActivity marks a session whose host could not be read, where there is
	// no host to bind the kind to.
	SSHActivity = "SSH"
)

// sshFlagsWithValue are the ssh options whose value is a separate argument, so
// `ssh -p 2222 prod-01` must not read 2222 as the destination.
//
// Everything else that starts with a dash is a switch, or carries its value
// attached (`-p2222`), and is skipped either way.
var sshFlagsWithValue = map[byte]struct{}{
	'B': {}, 'b': {}, 'c': {}, 'D': {}, 'E': {}, 'e': {}, 'F': {}, 'I': {},
	'i': {}, 'J': {}, 'L': {}, 'l': {}, 'm': {}, 'O': {}, 'o': {}, 'P': {},
	'p': {}, 'Q': {}, 'R': {}, 'S': {}, 'W': {}, 'w': {},
}

// SSH names a tab after the host it is connected to.
//
// A shell on a remote machine has a working directory and a terminal title like
// any other, and both describe the remote — but neither says which machine, and
// that is the thing a tab full of identical-looking shells needs to say. The
// host becomes the context, marked as remote: `ssh › prod-01`, and
// `ssh › prod-01 › Restart the queue workers` once the remote shell has
// something to report.
//
// The user is deliberately dropped: `root@prod-01` and `deploy@prod-01` are the
// same machine, and a tab bar has no room to say who is logged in.
type SSH struct{}

var _ Source = SSH{}

// NewSSH builds the source.
func NewSSH() SSH { return SSH{} }

func (SSH) Name() string    { return "ssh" }
func (SSH) Confidence() int { return ConfidenceSSH }

func (SSH) Resolve(pane *state.PaneState) (Parts, bool) {
	if pane == nil {
		return Parts{}, false
	}

	args, running := sshArgs(pane)
	if !running {
		return Parts{}, false
	}

	// A session whose destination cannot be read is still worth marking as
	// remote; the working directory then supplies the context and the mark has
	// to go in the activity instead.
	host := Sanitize(sshHost(args), 0)
	if host == "" {
		return Parts{Activity: SSHActivity}, true
	}
	return Parts{Context: qualify(host, sshKind)}, true
}

// sshArgs finds an ssh process in the pane and returns its arguments.
func sshArgs(pane *state.PaneState) ([]string, bool) {
	for _, process := range pane.Processes {
		if strings.EqualFold(process.Name, "ssh") {
			return process.Args, true
		}
	}
	return nil, false
}

// sshHost extracts the destination from an ssh command line.
//
// The destination is the first argument that is not an option or an option's
// value; everything after it is the remote command, which names the work rather
// than the machine and is left to the terminal title to report.
func sshHost(args []string) string {
	if len(args) == 0 {
		return ""
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			// Everything after this is positional.
			if i+1 < len(args) {
				return hostOf(args[i+1])
			}
			return ""
		case len(arg) > 1 && arg[0] == '-':
			// A flag whose value is a separate argument consumes the next one,
			// unless the value is attached: -p2222 carries its own.
			last := arg[len(arg)-1]
			if _, takesValue := sshFlagsWithValue[last]; takesValue {
				i++
			}
		case arg == "-":
			// Not a destination and not a flag; ignore it.
		default:
			return hostOf(arg)
		}
	}
	return ""
}

// hostOf reduces an ssh destination to the host alone, dropping the scheme, the
// user and the port from `ssh://deploy@prod-01:2222`.
func hostOf(destination string) string {
	host := strings.TrimSpace(destination)
	host = strings.TrimPrefix(host, "ssh://")
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	// A URL destination can carry a path.
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}

	// An IPv6 literal is bracketed, and only a port may follow the bracket.
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end >= 0 {
			return host[1:end]
		}
		return ""
	}
	// A bare colon is a port; a host with several is an unbracketed IPv6
	// literal, which has no port to strip.
	if strings.Count(host, ":") == 1 {
		host = host[:strings.Index(host, ":")]
	}
	return host
}
