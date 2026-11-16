package accounting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Meter lives in the proxy runtime, not the panel or subscription listener.
// One scheduler checks all sessions; no account starts a background goroutine.
type Meter struct {
	mu             sync.Mutex
	store          *Store
	runtime, epoch string
	pools          map[string]*pool
	handles        map[*Handle]struct{}
	closed         bool
	pending        sync.WaitGroup
	changed        chan struct{}
	waiters        int
}
type pool struct {
	id, account                           string
	available, inflight, upload, download int64
	reserved                              bool
}
type Handle struct {
	meter      *Meter
	pool       *pool
	credential string
	generation int64
	closer     io.Closer
	denied     bool
}

func NewMeter(store *Store, runtime string) *Meter {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return &Meter{store: store, runtime: runtime, epoch: hex.EncodeToString(id[:]), pools: map[string]*pool{}, handles: map[*Handle]struct{}{}, changed: make(chan struct{})}
}

func (m *Meter) Open(ctx context.Context, user, inbound string, closer io.Closer) (*Handle, error) {
	credential, generationText, ok := strings.Cut(user, ".")
	generation, err := strconv.ParseInt(generationText, 10, 64)
	if !ok || err != nil {
		return nil, ErrDenied
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.handles) >= 10000 {
		return nil, ErrDenied
	}
	var account string
	err = m.store.db.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN credentials c ON c.account=a.id WHERE c.id=? AND c.generation=? AND c.runtime=? AND c.inbound=? AND a.disabled=0 AND (a.expires IS NULL OR a.expires>?) AND (a.quota IS NULL OR a.charged<=a.quota)`, credential, generation, m.runtime, inbound, time.Now().Unix()).Scan(&account)
	if err != nil {
		return nil, ErrDenied
	}
	p := m.pools[account]
	if p == nil {
		p = &pool{id: m.epoch + "-" + account, account: account}
		m.pools[account] = p
	}
	h := &Handle{meter: m, pool: p, credential: credential, generation: generation, closer: closer}
	m.handles[h] = struct{}{}
	return h, nil
}

// Take grants at most want bytes; Complete returns unused bytes and records
// successful payload I/O. Neither the network nor SQLite lock is held together.
func (h *Handle) Take(want int) (int, error) {
	return h.take(want, false)
}

func (h *Handle) TakePacket(want int) (int, error) {
	return h.take(want, true)
}

func (h *Handle) take(want int, exact bool) (int, error) {
	m := h.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	if h.denied || m.closed {
		return 0, ErrDenied
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Checking at the byte boundary makes expiry independent of scheduler delays.
	if _, err := m.store.Authorized(ctx, h.credential, h.generation, time.Now()); err != nil {
		h.denied = true
		return 0, err
	}
	if want == 0 {
		return 0, nil
	}
	p := h.pool
	if exact && int64(want) > ReservationBytes {
		return 0, ErrDenied
	}
	for p.available == 0 || (exact && p.available < int64(want)) {
		room := ReservationBytes - p.available - p.inflight
		if room > 0 {
			n, err := m.store.Reserve(ctx, p.id, p.account, m.runtime, room, time.Now())
			if err != nil && p.inflight == 0 {
				return 0, err
			}
			if err == nil {
				p.available += n
				p.reserved = true
			}
		}
		if p.available > 0 && (!exact || p.available >= int64(want)) {
			break
		}
		if p.inflight == 0 {
			return 0, ErrDenied
		}
		changed := m.changed
		m.waiters++
		m.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		m.mu.Lock()
		m.waiters--
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if h.denied || m.closed {
			return 0, ErrDenied
		}
		if _, err := m.store.Authorized(ctx, h.credential, h.generation, time.Now()); err != nil {
			return 0, err
		}
	}
	n := min(int64(want), p.available)
	p.available -= n
	p.inflight += n
	m.pending.Add(1)
	return int(n), nil
}
func (h *Handle) Complete(granted, actual int, upload bool) {
	if granted == 0 {
		return
	}
	m := h.meter
	defer m.pending.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	actual = max(0, min(granted, actual))
	h.pool.available += int64(granted - actual)
	h.pool.inflight -= int64(granted)
	if m.waiters > 0 {
		close(m.changed)
		m.changed = make(chan struct{})
	}
	if upload {
		h.pool.upload += int64(actual)
	} else {
		h.pool.download += int64(actual)
	}
}
func (h *Handle) Close() {
	m := h.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	h.denied = true
	delete(m.handles, h)
}

func (m *Meter) Tick(ctx context.Context) error {
	m.mu.Lock()
	var toClose []io.Closer
	var result error
	checked := map[string]error{}
	for h := range m.handles {
		key := h.credential + "." + strconv.FormatInt(h.generation, 10)
		err, found := checked[key]
		if !found {
			_, err = m.store.Authorized(ctx, h.credential, h.generation, time.Now())
			checked[key] = err
		}
		if err != nil {
			h.denied = true
			toClose = append(toClose, h.closer)
			delete(m.handles, h)
		}
	}
	for _, p := range m.pools {
		if p.reserved {
			result = errors.Join(result, m.store.Checkpoint(ctx, p.id, p.upload, p.download, false))
		}
	}
	if result != nil {
		for h := range m.handles {
			h.denied = true
			toClose = append(toClose, h.closer)
			delete(m.handles, h)
		}
	}
	m.mu.Unlock()
	for _, c := range toClose {
		_ = c.Close()
	}
	return result
}

func (m *Meter) Run(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.Tick(ctx); err != nil {
				return err
			}
		}
	}
}

// Shutdown closes sockets first; callers must finish outstanding I/O before
// Close settles the remaining reservation. A forced exit leaves it charged.
func (m *Meter) Shutdown() {
	m.mu.Lock()
	m.closed = true
	close(m.changed)
	m.changed = make(chan struct{})
	closers := []io.Closer{}
	for h := range m.handles {
		h.denied = true
		closers = append(closers, h.closer)
	}
	m.mu.Unlock()
	for _, c := range closers {
		_ = c.Close()
	}
}
func (m *Meter) Close(ctx context.Context) error {
	done := make(chan struct{})
	go func() { m.pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result error
	for _, p := range m.pools {
		if p.reserved {
			result = errors.Join(result, m.store.Checkpoint(ctx, p.id, p.upload, p.download, true))
		}
	}
	return result
}
