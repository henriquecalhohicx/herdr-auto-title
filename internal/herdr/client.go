package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// SocketPathEnv names the environment variable holding the path to the Herdr
// socket. The path is never hard-coded.
const SocketPathEnv = "HERDR_SOCKET_PATH"

// eventBuffer bounds the queue between the socket reader and the router. The
// router only updates the cache and arms a timer, so it drains far faster than
// Herdr fills; a full buffer therefore means the router is wedged, and blocking
// is preferable to silently losing state.
const eventBuffer = 256

// ErrClosed is returned once the client has been shut down.
var ErrClosed = errors.New("herdr: client closed")

// Client is the subset of the Herdr socket API that Auto Title uses.
type Client interface {
	// Call issues one request and decodes its result into result, which may be
	// nil when the caller does not need it.
	Call(ctx context.Context, method string, params any, result any) error
	// Subscribe opens the event stream. It may be called only once.
	Subscribe(ctx context.Context, subs []Subscription) error
	// Events yields broadcast events. It is closed when the stream ends.
	Events() <-chan Event
	// Err reports why the stream ended, or nil while it is healthy.
	Err() error
	// Close tears the client down. It is safe to call more than once.
	Close() error
}

// SocketClient speaks NDJSON to the Herdr socket.
//
// Herdr serves one request per connection: after answering a method it closes
// the connection, and sending anything on a subscription connection ends the
// stream. So each Call dials its own short-lived connection, and Subscribe owns
// a separate connection that is only ever read from.
type SocketClient struct {
	log  *slog.Logger
	path string

	seq atomic.Uint64

	mu            sync.Mutex
	streamConn    net.Conn
	subscribed    bool
	readerRunning bool
	closed        bool

	events    chan Event
	done      chan struct{}
	closeOnce sync.Once

	errMu sync.Mutex
	err   error
}

var _ Client = (*SocketClient)(nil)

// SocketPath returns the configured Herdr socket path.
func SocketPath() (string, error) {
	path := os.Getenv(SocketPathEnv)
	if path == "" {
		return "", fmt.Errorf("%s is not set: Auto Title must be started by Herdr", SocketPathEnv)
	}
	return path, nil
}

// New builds a client for the socket named by HERDR_SOCKET_PATH. It performs no
// I/O; the first connection is made by the first call.
func New(log *slog.Logger) (*SocketClient, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	return NewWithPath(log, path), nil
}

// NewWithPath builds a client for an explicit socket path.
func NewWithPath(log *slog.Logger, path string) *SocketClient {
	return &SocketClient{
		log:    log,
		path:   path,
		events: make(chan Event, eventBuffer),
		done:   make(chan struct{}),
	}
}

func (c *SocketClient) Events() <-chan Event { return c.events }

func (c *SocketClient) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

func (c *SocketClient) Close() error {
	c.shutdown(ErrClosed)
	return nil
}

// shutdown records why the client stopped and drops the event stream. Only the
// first call takes effect.
//
// The event channel is closed by whoever can guarantee no further sends: the
// reader goroutine if one is running, otherwise shutdown itself.
func (c *SocketClient) shutdown(cause error) {
	c.closeOnce.Do(func() {
		c.errMu.Lock()
		c.err = cause
		c.errMu.Unlock()

		c.mu.Lock()
		c.closed = true
		conn := c.streamConn
		c.streamConn = nil
		hasReader := c.readerRunning
		c.mu.Unlock()

		close(c.done)
		if conn != nil {
			// Closing the connection is what makes the reader return.
			_ = conn.Close()
		}
		if !hasReader {
			close(c.events)
		}
	})
}

// dial opens one connection to the Herdr socket.
//
// Herdr's local transport is platform-specific; only the Unix domain socket
// form is implemented here.
func (c *SocketClient) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("connect to herdr socket %s: %w", c.path, err)
	}
	return conn, nil
}

func (c *SocketClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Call sends one request on a connection of its own and reads the single
// response Herdr answers with before closing.
func (c *SocketClient) Call(ctx context.Context, method string, params any, result any) error {
	if c.isClosed() {
		return ErrClosed
	}
	if params == nil {
		params = emptyParams{}
	}

	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Unblock a call whose context is cancelled while it waits on the socket.
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	req := request{
		ID:     fmt.Sprintf("auto-title-%d", c.seq.Add(1)),
		Method: method,
		Params: params,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return withContextErr(ctx, fmt.Errorf("send %s: %w", method, err))
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return withContextErr(ctx, fmt.Errorf("read %s response: %w", method, err))
	}

	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	if f.Error != nil {
		return fmt.Errorf("%s: %w", method, f.Error)
	}
	if result != nil && len(f.Result) > 0 {
		if err := json.Unmarshal(f.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

// withContextErr reports cancellation as such rather than as the socket error
// that closing the connection produced.
func withContextErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// Subscribe opens the event stream on a dedicated connection. Nothing may be
// sent on that connection afterwards, so it is read-only for its lifetime.
func (c *SocketClient) Subscribe(ctx context.Context, subs []Subscription) error {
	c.mu.Lock()
	switch {
	case c.closed:
		c.mu.Unlock()
		return ErrClosed
	case c.subscribed:
		c.mu.Unlock()
		return errors.New("herdr: already subscribed")
	}
	c.subscribed = true
	c.mu.Unlock()

	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}

	req := request{
		ID:     fmt.Sprintf("auto-title-%d", c.seq.Add(1)),
		Method: MethodEventsSubscribe,
		Params: subscribeParams{Subscriptions: subs},
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		conn.Close()
		return fmt.Errorf("send %s: %w", MethodEventsSubscribe, err)
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		conn.Close()
		return fmt.Errorf("read %s response: %w", MethodEventsSubscribe, err)
	}

	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		conn.Close()
		return fmt.Errorf("decode %s response: %w", MethodEventsSubscribe, err)
	}
	if f.Error != nil {
		conn.Close()
		return fmt.Errorf("%s: %w", MethodEventsSubscribe, f.Error)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.Close()
		return ErrClosed
	}
	c.streamConn = conn
	c.readerRunning = true
	c.mu.Unlock()

	go c.readEvents(reader)
	return nil
}

// readEvents consumes the stream one line at a time. It never does expensive
// work: events are queued for the router.
func (c *SocketClient) readEvents(reader *bufio.Reader) {
	// The reader is the only sender, so it owns closing the channel.
	defer close(c.events)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = fmt.Errorf("herdr event stream closed: %w", err)
			}
			c.shutdown(err)
			return
		}
	}
}

func (c *SocketClient) dispatch(line []byte) {
	var f frame
	if err := json.Unmarshal(line, &f); err != nil {
		c.log.Warn("discarding unparseable frame", "error", err)
		return
	}
	if !f.isEvent() {
		// Nothing else is expected on a subscription connection.
		c.log.Debug("ignoring non-event frame on the event stream", "id", f.ID)
		return
	}

	select {
	case c.events <- Event{Kind: f.Event, Data: f.Data}:
	case <-c.done:
	}
}

// SessionSnapshot fetches the full session state used to seed the cache.
func SessionSnapshot(ctx context.Context, c Client) (Snapshot, error) {
	var res snapshotResult
	if err := c.Call(ctx, MethodSessionSnapshot, emptyParams{}, &res); err != nil {
		return Snapshot{}, err
	}
	return res.Snapshot, nil
}

// GetTab reads a tab's current state.
//
// Reading is how Auto Title learns what a tab holds. Events are only triggers:
// Herdr replays a backlog of them on subscribe, so an event payload describes
// some past moment, while a read always answers with the present.
func GetTab(ctx context.Context, c Client, tabID string) (TabInfo, error) {
	var res tabInfoResult
	if err := c.Call(ctx, MethodTabGet, TabTarget{TabID: tabID}, &res); err != nil {
		return TabInfo{}, err
	}
	return res.Tab, nil
}

// GetPane reads a pane's current state.
func GetPane(ctx context.Context, c Client, paneID string) (PaneInfo, error) {
	var res paneInfoResult
	if err := c.Call(ctx, MethodPaneGet, PaneTarget{PaneID: paneID}, &res); err != nil {
		return PaneInfo{}, err
	}
	return res.Pane, nil
}

// RenameTab sets a tab's label.
func RenameTab(ctx context.Context, c Client, tabID, label string) error {
	return c.Call(ctx, MethodTabRename, TabRenameParams{TabID: tabID, Label: label}, nil)
}
