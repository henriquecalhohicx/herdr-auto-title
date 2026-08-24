package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// incoming is one request as the test server saw it.
type incoming struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// testServer imitates Herdr: one request per connection, except a subscribe,
// after which the connection stays open and carries events.
type testServer struct {
	t    *testing.T
	ln   net.Listener
	path string

	mu          sync.Mutex
	requests    []incoming
	streams     []net.Conn
	connections int

	// reply returns the line to send back, and whether to keep the connection
	// open as an event stream.
	reply func(req incoming) (string, bool)
}

func newTestServer(t *testing.T, reply func(incoming) (string, bool)) *testServer {
	t.Helper()

	// Unix socket paths are short; keep well clear of the platform limit.
	dir, err := os.MkdirTemp("", "at")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	path := filepath.Join(dir, "h.sock")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}

	s := &testServer{t: t, ln: ln, path: path, reply: reply}
	go s.accept()
	t.Cleanup(func() {
		ln.Close()
		os.RemoveAll(dir)
		s.mu.Lock()
		for _, conn := range s.streams {
			conn.Close()
		}
		s.mu.Unlock()
	})
	return s
}

func (s *testServer) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *testServer) serve(conn net.Conn) {
	s.mu.Lock()
	s.connections++
	s.mu.Unlock()

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		conn.Close()
		return
	}

	var req incoming
	if err := json.Unmarshal(line, &req); err != nil {
		conn.Close()
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, req)
	reply := s.reply
	s.mu.Unlock()

	response, keepOpen := reply(req)
	if response != "" {
		if _, err := io.WriteString(conn, response+"\n"); err != nil {
			conn.Close()
			return
		}
	}
	if !keepOpen {
		// Herdr closes the connection once a method has been answered.
		conn.Close()
		return
	}

	s.mu.Lock()
	s.streams = append(s.streams, conn)
	s.mu.Unlock()
}

func (s *testServer) client() *SocketClient {
	c := NewWithPath(discardLogger(), s.path)
	s.t.Cleanup(func() { c.Close() })
	return c
}

// pushEvent writes a line on the subscription connection.
func (s *testServer) pushEvent(line string) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		streams := append([]net.Conn(nil), s.streams...)
		s.mu.Unlock()
		if len(streams) > 0 {
			io.WriteString(streams[0], line+"\n")
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatal("no subscription connection to push an event on")
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *testServer) closeStream() {
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		streams := append([]net.Conn(nil), s.streams...)
		s.mu.Unlock()
		if len(streams) > 0 {
			streams[0].Close()
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatal("no subscription connection to close")
		}
		time.Sleep(time.Millisecond)
	}
}

func (s *testServer) connectionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connections
}

func (s *testServer) seen() []incoming {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]incoming(nil), s.requests...)
}

// respondOK answers every method with an empty result and closes.
func respondOK(req incoming) (string, bool) {
	if req.Method == MethodEventsSubscribe {
		return `{"id":"` + req.ID + `","result":{"type":"subscription_started"}}`, true
	}
	return `{"id":"` + req.ID + `","result":{}}`, false
}

func TestCallDecodesResult(t *testing.T) {
	srv := newTestServer(t, func(req incoming) (string, bool) {
		return `{"id":"` + req.ID + `","result":{"version":"0.8.2"}}`, false
	})

	var got struct {
		Version string `json:"version"`
	}
	if err := srv.client().Call(context.Background(), MethodPing, nil, &got); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got.Version != "0.8.2" {
		t.Errorf("version = %q, want 0.8.2", got.Version)
	}

	seen := srv.seen()
	if len(seen) != 1 || seen[0].Method != MethodPing {
		t.Fatalf("server saw %+v, want one ping", seen)
	}
	// Herdr requires params on every request.
	if string(seen[0].Params) != "{}" {
		t.Errorf("params = %s, want {}", seen[0].Params)
	}
}

func TestEachCallUsesItsOwnConnection(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()

	// Herdr closes the connection after answering, so a reused connection would
	// fail on the second call.
	for i := 0; i < 3; i++ {
		if err := client.Call(context.Background(), MethodPing, nil, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := srv.connectionCount(); got != 3 {
		t.Errorf("server accepted %d connections, want 3", got)
	}
}

func TestCallReturnsAPIError(t *testing.T) {
	srv := newTestServer(t, func(req incoming) (string, bool) {
		return `{"id":"` + req.ID + `","error":{"code":"not_found","message":"no such tab"}}`, false
	})

	err := RenameTab(context.Background(), srv.client(), "wE:t1", "dashboard")
	if err == nil {
		t.Fatal("Call succeeded, want an error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.Code != "not_found" {
		t.Errorf("code = %q, want not_found", apiErr.Code)
	}
}

func TestCallReportsAnUncorrelatedError(t *testing.T) {
	// Herdr answers a malformed request with an error frame carrying no id and
	// then drops the connection.
	srv := newTestServer(t, func(incoming) (string, bool) {
		return `{"id":"","error":{"code":"invalid_request","message":"unknown variant"}}`, false
	})

	if err := srv.client().Call(context.Background(), MethodEventsSubscribe, nil, nil); err == nil {
		t.Error("Call succeeded despite an error frame")
	}
}

func TestCallReportsAClosedConnection(t *testing.T) {
	srv := newTestServer(t, func(incoming) (string, bool) { return "", false })

	if err := srv.client().Call(context.Background(), MethodPing, nil, nil); err == nil {
		t.Error("Call succeeded despite the connection closing without a response")
	}
}

func TestCallRespectsContext(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	srv := newTestServer(t, func(incoming) (string, bool) {
		<-release
		return "", false
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.client().Call(ctx, MethodPing, nil, nil) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call ignored its cancelled context")
	}
}

func TestSubscribeDeliversEvents(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()

	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubPaneUpdated}, {Type: SubTabCreated}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	seen := srv.seen()
	if len(seen) != 1 || seen[0].Method != MethodEventsSubscribe {
		t.Fatalf("server saw %+v, want one subscribe", seen)
	}
	// Subscription types are dot-separated even though events arrive snake_case.
	params := string(seen[0].Params)
	if !strings.Contains(params, `"pane.updated"`) || !strings.Contains(params, `"tab.created"`) {
		t.Errorf("params = %s, want dot-notation subscription types", params)
	}

	srv.pushEvent(`{"event":"pane_updated","data":{"type":"pane_updated","pane":{"pane_id":"wE:p1","tab_id":"wE:t1","cwd":"/work/api"}}}`)

	select {
	case event := <-client.Events():
		if event.Kind != EventPaneUpdated {
			t.Fatalf("event kind = %q, want %q", event.Kind, EventPaneUpdated)
		}
		var payload PaneUpdatedData
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if payload.Pane.CWD != "/work/api" {
			t.Errorf("cwd = %q, want /work/api", payload.Pane.CWD)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the event")
	}
}

func TestSubscribeReportsRejection(t *testing.T) {
	srv := newTestServer(t, func(incoming) (string, bool) {
		return `{"id":"","error":{"code":"invalid_request","message":"unknown variant ` + "`pane.output_changed`" + `"}}`, false
	})

	err := srv.client().Subscribe(context.Background(), []Subscription{{Type: "pane.output_changed"}})
	if err == nil {
		t.Fatal("Subscribe succeeded despite a rejection")
	}
	if !strings.Contains(err.Error(), "invalid_request") {
		t.Errorf("error = %v, want it to mention invalid_request", err)
	}
}

func TestSubscribeOnlyOnce(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()

	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubTabCreated}}); err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubTabCreated}}); err == nil {
		t.Error("second Subscribe succeeded, want an error")
	}
}

func TestUnparseableFrameIsSkipped(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()
	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubTabCreated}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	srv.pushEvent(`{not json`)
	srv.pushEvent(`{"event":"tab_created","data":{"type":"tab_created","tab":{"tab_id":"wE:t1"}}}`)

	select {
	case event := <-client.Events():
		if event.Kind != EventTabCreated {
			t.Errorf("event kind = %q, want %q", event.Kind, EventTabCreated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a malformed frame stopped the reader")
	}
}

func TestLostStreamClosesEvents(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()
	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubTabCreated}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	srv.closeStream()

	select {
	case _, ok := <-client.Events():
		if ok {
			t.Error("an event arrived after the stream was closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the event channel stayed open after the stream was closed")
	}
	if client.Err() == nil {
		t.Error("Err is nil after the stream was closed")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()

	if err := client.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, ok := <-client.Events(); ok {
		t.Error("the event channel stayed open after Close")
	}
	if err := client.Call(context.Background(), MethodPing, nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Call after Close returned %v, want ErrClosed", err)
	}
}

func TestCloseEndsAnActiveStream(t *testing.T) {
	srv := newTestServer(t, respondOK)
	client := srv.client()
	if err := client.Subscribe(context.Background(), []Subscription{{Type: SubTabCreated}}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	client.Close()

	select {
	case _, ok := <-client.Events():
		if ok {
			t.Error("an event arrived after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the event channel stayed open after Close")
	}
}

func TestSessionSnapshotDecodesTheWrapper(t *testing.T) {
	srv := newTestServer(t, func(req incoming) (string, bool) {
		return `{"id":"` + req.ID + `","result":{"snapshot":{"version":"0.8.2","protocol":20,` +
			`"tabs":[{"tab_id":"wE:t1","workspace_id":"wE","label":"1","number":1}],` +
			`"panes":[{"pane_id":"wE:p1","tab_id":"wE:t1","terminal_id":"t","workspace_id":"wE","cwd":"/work/api","focused":true}]}}}`, false
	})

	snapshot, err := SessionSnapshot(context.Background(), srv.client())
	if err != nil {
		t.Fatalf("SessionSnapshot: %v", err)
	}
	if snapshot.Version != "0.8.2" || snapshot.Protocol != 20 {
		t.Errorf("version/protocol = %q/%d, want 0.8.2/20", snapshot.Version, snapshot.Protocol)
	}
	if len(snapshot.Tabs) != 1 || snapshot.Tabs[0].Label != "1" {
		t.Errorf("tabs = %+v, want one tab labelled 1", snapshot.Tabs)
	}
	if len(snapshot.Panes) != 1 || snapshot.Panes[0].CWD != "/work/api" {
		t.Errorf("panes = %+v, want one pane in /work/api", snapshot.Panes)
	}
}

func TestRenameTabSendsTabAndLabel(t *testing.T) {
	srv := newTestServer(t, respondOK)

	if err := RenameTab(context.Background(), srv.client(), "wE:t1", "dashboard · Tests"); err != nil {
		t.Fatalf("RenameTab: %v", err)
	}

	seen := srv.seen()
	if len(seen) != 1 || seen[0].Method != MethodTabRename {
		t.Fatalf("server saw %+v, want one tab.rename", seen)
	}
	var params TabRenameParams
	if err := json.Unmarshal(seen[0].Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.TabID != "wE:t1" || params.Label != "dashboard · Tests" {
		t.Errorf("params = %+v, want {wE:t1 dashboard · Tests}", params)
	}
}

func TestNullFieldsDecodeAsEmpty(t *testing.T) {
	var payload PaneUpdatedData
	raw := `{"type":"pane_updated","pane":{"pane_id":"wE:p1","tab_id":"wE:t1","cwd":null,"terminal_title":null,"agent":null}}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Pane.CWD != "" || payload.Pane.TerminalTitle != "" || payload.Pane.Agent != "" {
		t.Errorf("null fields decoded as %+v, want empty strings", payload.Pane)
	}
}

func TestSocketPathRequiresTheEnvironment(t *testing.T) {
	t.Setenv(SocketPathEnv, "")
	if _, err := SocketPath(); err == nil {
		t.Error("SocketPath succeeded without the environment variable")
	}

	t.Setenv(SocketPathEnv, "/tmp/herdr.sock")
	got, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if got != "/tmp/herdr.sock" {
		t.Errorf("path = %q, want /tmp/herdr.sock", got)
	}
}

func TestErrorCode(t *testing.T) {
	srv := newTestServer(t, func(req incoming) (string, bool) {
		return `{"id":"` + req.ID + `","error":{"code":"tab_not_found","message":"tab wE:t1 not found"}}`, false
	})

	err := RenameTab(context.Background(), srv.client(), "wE:t1", "dashboard")
	if got := ErrorCode(err); got != CodeTabNotFound {
		t.Errorf("ErrorCode = %q, want %q", got, CodeTabNotFound)
	}
	if got := ErrorCode(errors.New("plain")); got != "" {
		t.Errorf("ErrorCode of a plain error = %q, want empty", got)
	}
}
