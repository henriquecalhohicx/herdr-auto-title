package debounce

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder counts fires per key.
type recorder struct {
	mu     sync.Mutex
	counts map[string]int
	total  atomic.Int64
}

func newRecorder() *recorder {
	return &recorder{counts: make(map[string]int)}
}

func (r *recorder) fire(key string) {
	r.mu.Lock()
	r.counts[key]++
	r.mu.Unlock()
	r.total.Add(1)
}

func (r *recorder) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[key]
}

// await blocks until key has fired at least want times.
func (r *recorder) await(t *testing.T, key string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.count(key) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q to fire %d times, saw %d", key, want, r.count(key))
}

func TestBurstCollapsesToOneFire(t *testing.T) {
	rec := newRecorder()
	m := New(50*time.Millisecond, 0, rec.fire)
	defer m.Close()

	// Ten events well inside one debounce window.
	for i := 0; i < 10; i++ {
		m.Schedule("wE:t1")
		time.Sleep(2 * time.Millisecond)
	}

	rec.await(t, "wE:t1", 1)
	time.Sleep(100 * time.Millisecond)

	if got := rec.count("wE:t1"); got != 1 {
		t.Errorf("burst produced %d reconciliations, want 1", got)
	}
}

func TestSeparateBurstsFireSeparately(t *testing.T) {
	rec := newRecorder()
	m := New(30*time.Millisecond, 0, rec.fire)
	defer m.Close()

	m.Schedule("wE:t1")
	rec.await(t, "wE:t1", 1)
	m.Schedule("wE:t1")
	rec.await(t, "wE:t1", 2)

	if got := rec.count("wE:t1"); got != 2 {
		t.Errorf("two settled bursts produced %d reconciliations, want 2", got)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	rec := newRecorder()
	m := New(30*time.Millisecond, 0, rec.fire)
	defer m.Close()

	m.Schedule("wE:t1")
	m.Schedule("wE:t2")
	m.Schedule("wE:t3")

	rec.await(t, "wE:t1", 1)
	rec.await(t, "wE:t2", 1)
	rec.await(t, "wE:t3", 1)

	for _, key := range []string{"wE:t1", "wE:t2", "wE:t3"} {
		if got := rec.count(key); got != 1 {
			t.Errorf("%s fired %d times, want 1", key, got)
		}
	}
}

func TestCancelPreventsFiring(t *testing.T) {
	rec := newRecorder()
	m := New(50*time.Millisecond, 0, rec.fire)
	defer m.Close()

	m.Schedule("wE:t1")
	m.Schedule("wE:t2")
	m.Cancel("wE:t1")

	rec.await(t, "wE:t2", 1)
	time.Sleep(60 * time.Millisecond)

	if got := rec.count("wE:t1"); got != 0 {
		t.Errorf("a cancelled key fired %d times, want 0", got)
	}
}

func TestPendingReportsArmedTimers(t *testing.T) {
	rec := newRecorder()
	m := New(50*time.Millisecond, 0, rec.fire)
	defer m.Close()

	if m.Pending("wE:t1") {
		t.Error("an unscheduled key reports as pending")
	}
	m.Schedule("wE:t1")
	if !m.Pending("wE:t1") {
		t.Error("a scheduled key does not report as pending")
	}
	m.Cancel("wE:t1")
	if m.Pending("wE:t1") {
		t.Error("a cancelled key still reports as pending")
	}
}

func TestCloseStopsEverything(t *testing.T) {
	rec := newRecorder()
	m := New(30*time.Millisecond, 0, rec.fire)

	m.Schedule("wE:t1")
	m.Schedule("wE:t2")
	m.Close()

	time.Sleep(80 * time.Millisecond)
	if got := rec.total.Load(); got != 0 {
		t.Errorf("%d keys fired after Close, want 0", got)
	}

	// Scheduling after Close is a no-op rather than a panic.
	m.Schedule("wE:t3")
	time.Sleep(60 * time.Millisecond)
	if got := rec.total.Load(); got != 0 {
		t.Errorf("%d keys fired after scheduling on a closed manager, want 0", got)
	}
}

func TestCloseWaitsForRunningAction(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool

	m := New(10*time.Millisecond, 0, func(string) {
		close(started)
		<-release
		finished.Store(true)
	})

	m.Schedule("wE:t1")
	<-started

	closed := make(chan struct{})
	go func() {
		m.Close()
		close(closed)
	}()

	select {
	case <-closed:
		t.Fatal("Close returned while an action was still running")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the action finished")
	}
	if !finished.Load() {
		t.Error("Close returned before the action completed")
	}
}

func TestDefaultDelayApplies(t *testing.T) {
	m := New(0, 0, func(string) {})
	defer m.Close()

	if m.delay != DefaultDelay {
		t.Errorf("delay = %s, want %s", m.delay, DefaultDelay)
	}
}

func TestMaxWaitFiresDuringAContinuousBurst(t *testing.T) {
	rec := newRecorder()
	m := New(50*time.Millisecond, 150*time.Millisecond, rec.fire)
	defer m.Close()

	// A pane running an agent rearms its tab faster than the quiet window for
	// as long as the agent works. Without a cap the key would never fire.
	stop := time.After(500 * time.Millisecond)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	fired := false
	for !fired {
		select {
		case <-ticker.C:
			m.Schedule("wE:t1")
			if rec.count("wE:t1") > 0 {
				fired = true
			}
		case <-stop:
			t.Fatalf("nothing fired during a continuous burst, count = %d", rec.count("wE:t1"))
		}
	}
}

func TestMaxWaitDoesNotBreakBurstCollapsing(t *testing.T) {
	rec := newRecorder()
	m := New(50*time.Millisecond, 500*time.Millisecond, rec.fire)
	defer m.Close()

	// A burst that settles well inside the cap must still collapse to one fire.
	for i := 0; i < 10; i++ {
		m.Schedule("wE:t1")
		time.Sleep(2 * time.Millisecond)
	}

	rec.await(t, "wE:t1", 1)
	time.Sleep(100 * time.Millisecond)

	if got := rec.count("wE:t1"); got != 1 {
		t.Errorf("burst produced %d reconciliations, want 1", got)
	}
}
