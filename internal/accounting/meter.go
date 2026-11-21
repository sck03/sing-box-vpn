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
	"sync/atomic"
	"time"
)

const AuthorizationInterval = 250 * time.Millisecond

// Meter owns topology only. Each account/period pool serializes its own
// reservations; neither SQLite nor socket I/O holds the topology lock.
type Meter struct {
	mu             sync.Mutex
	maintenance    sync.Mutex
	store          *Store
	runtime, epoch string
	pools          map[string]*pool
	handles        map[*Handle]struct{}
	auth           map[string]*authorization
	closed         bool
	pending        sync.WaitGroup
	poolSequence   uint64
	retired        uint64
	detached       []*pool      // idle pools retained until their final checkpoint succeeds
	scratch        meterScratch // reused only under maintenance; cleared after each tick
}

type poolSnapshot struct {
	key   string
	value *pool
}

type meterScratch struct {
	handles         []*Handle
	pools           []poolSnapshot
	wanted          map[authorizationKey]bool
	authorizations  map[*authorization]bool
	checkpoints     []UsageCheckpoint
	checkpointPools []*pool
	snapshot        authorizationRead
}

func (s *meterScratch) reset() {
	s.snapshot.reset()
	clear(s.handles)
	clear(s.pools)
	clear(s.wanted)
	clear(s.authorizations)
	clear(s.checkpoints)
	clear(s.checkpointPools)
	s.handles = s.handles[:0]
	s.pools = s.pools[:0]
	s.checkpoints = s.checkpoints[:0]
	s.checkpointPools = s.checkpointPools[:0]
}

type pool struct {
	mu                                    sync.Mutex
	id, account                           string
	available, inflight, upload, download int64
	savedUpload, savedDownload            int64
	reserved                              bool
	period                                int64
	sequence                              uint64
	refs                                  int // protected by Meter.mu
	changed                               chan struct{}
	waiters                               int
}
type authorization struct {
	mu         sync.Mutex
	credential string
	generation int64
	usage      Usage
	until      time.Time
	epoch      uint64
	err        error
	checkedAt  time.Time
}
type Handle struct {
	meter  *Meter
	pool   *pool
	auth   *authorization
	closer io.Closer
	denied atomic.Bool
}

func NewMeter(store *Store, runtime string) *Meter {
	var id [16]byte
	_, _ = rand.Read(id[:])
	return &Meter{store: store, runtime: runtime, epoch: hex.EncodeToString(id[:]), pools: map[string]*pool{}, handles: map[*Handle]struct{}{}, auth: map[string]*authorization{}}
}

func (a *authorization) check(ctx context.Context, store *Store, force bool) (Usage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	epoch := store.authorizationEpoch.Load()
	if force || epoch != a.epoch || !now.Before(a.until) {
		query, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		a.usage, a.err = store.authorize(query, a.credential, a.generation, now, false)
		a.setDeadline(now, epoch)
	}
	return a.usage, a.err
}

func (a *authorization) setDeadline(now time.Time, epoch uint64) {
	a.epoch, a.checkedAt, a.until = epoch, now, now.Add(AuthorizationInterval)
	for _, deadline := range []*time.Time{a.usage.EffectiveExpiresAt, a.usage.NextReset} {
		if deadline != nil && deadline.Before(a.until) {
			a.until = *deadline
		}
	}
}

func (m *Meter) Open(ctx context.Context, user, inbound string, closer io.Closer) (*Handle, error) {
	credential, generationText, ok := strings.Cut(user, ".")
	generation, err := strconv.ParseInt(generationText, 10, 64)
	if !ok || err != nil {
		return nil, ErrDenied
	}
	var binding int
	if err = m.store.db.QueryRowContext(ctx, `SELECT 1 FROM credentials WHERE id=? AND generation=? AND runtime=? AND inbound=?`, credential, generation, m.runtime, inbound).Scan(&binding); err != nil {
		return nil, ErrDenied
	}
	u, err := m.store.authorize(ctx, credential, generation, time.Now(), true)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || len(m.handles) >= 10000 {
		return nil, ErrDenied
	}
	key := u.ID + "-" + strconv.FormatInt(u.PeriodID, 10)
	p := m.pools[key]
	if p == nil {
		m.poolSequence++
		p = &pool{id: m.epoch + "/" + strconv.FormatUint(m.poolSequence, 10), sequence: m.poolSequence, account: u.ID, period: u.PeriodID, changed: make(chan struct{})}
		m.pools[key] = p
	}
	a := m.auth[user]
	if a == nil {
		a = &authorization{credential: credential, generation: generation}
		m.auth[user] = a
	}
	h := &Handle{meter: m, pool: p, auth: a, closer: closer}
	p.refs++
	m.handles[h] = struct{}{}
	return h, nil
}

func (h *Handle) authorized(ctx context.Context, force bool) error {
	if h.denied.Load() {
		return ErrDenied
	}
	u, err := h.auth.check(ctx, h.meter.store, force)
	if err == nil && u.PeriodID != h.pool.period {
		err = ErrDenied
	}
	if err != nil {
		h.denied.Store(true)
	}
	return err
}
func (h *Handle) Take(want int) (int, error)       { return h.take(want, false) }
func (h *Handle) TakePacket(want int) (int, error) { return h.take(want, true) }

func (h *Handle) take(want int, exact bool) (int, error) {
	if want < 0 || (exact && int64(want) > ReservationBytes) {
		return 0, ErrDenied
	}
	// A cache hit performs no SQLite work or timer allocation.
	if err := h.authorized(context.Background(), false); err != nil {
		return 0, err
	}
	if want == 0 {
		return 0, nil
	}
	m, p := h.meter, h.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	var ctx context.Context
	for p.available == 0 || (exact && p.available < int64(want)) {
		if h.denied.Load() {
			return 0, ErrDenied
		}
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}
		room := ReservationBytes - p.available - p.inflight
		if room > 0 {
			n, err := m.store.reservePeriod(ctx, p.id, p.account, m.runtime, room, time.Now(), p.period)
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
		changed := p.changed
		p.waiters++
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		p.mu.Lock()
		p.waiters--
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if err := h.authorized(ctx, false); err != nil {
			return 0, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || h.denied.Load() {
		return 0, ErrDenied
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
	defer h.meter.pending.Done()
	p := h.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	actual = max(0, min(granted, actual))
	p.available += int64(granted - actual)
	p.inflight -= int64(granted)
	if upload {
		p.upload += int64(actual)
	} else {
		p.download += int64(actual)
	}
	if p.waiters > 0 {
		close(p.changed)
		p.changed = make(chan struct{})
	}
}
func (h *Handle) Close() {
	h.denied.Store(true)
	m := h.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.handles[h]; ok {
		delete(m.handles, h)
		h.pool.refs--
	}
}

func (m *Meter) Tick(ctx context.Context) error {
	m.maintenance.Lock()
	defer m.maintenance.Unlock()
	scratch := &m.scratch
	defer scratch.reset()
	if scratch.wanted == nil {
		scratch.wanted = make(map[authorizationKey]bool)
		scratch.authorizations = make(map[*authorization]bool)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrDenied
	}
	for h := range m.handles {
		scratch.handles = append(scratch.handles, h)
	}
	for key, p := range m.pools {
		scratch.pools = append(scratch.pools, poolSnapshot{key, p})
	}
	m.mu.Unlock()
	handles, wanted, authorizations := scratch.handles, scratch.wanted, scratch.authorizations
	for _, h := range handles {
		wanted[authorizationKey{h.auth.credential, h.auth.generation}] = true
		authorizations[h.auth] = true
	}
	if len(wanted) > 0 {
		now, epoch := time.Now(), m.store.authorizationEpoch.Load()
		snapshot, err := m.store.authorizationSnapshot(ctx, m.runtime, now, wanted, &scratch.snapshot)
		if err != nil {
			m.Shutdown()
			return err
		}
		for a := range authorizations {
			a.mu.Lock()
			if !a.checkedAt.After(now) {
				usage, ok := snapshot[authorizationKey{a.credential, a.generation}]
				a.usage = Usage{}
				if ok {
					a.usage = *usage
				}
				a.err = nil
				if !ok {
					a.err = ErrDenied
				}
				a.setDeadline(now, epoch)
			}
			a.mu.Unlock()
		}
	} else {
		// An idle runtime need not retain a previous connection peak's buffers.
		scratch.snapshot = authorizationRead{}
	}
	for _, h := range handles {
		if err := h.authorized(ctx, false); err != nil {
			h.Close()
			_ = h.closer.Close()
		}
	}
	var result error
	for _, entry := range scratch.pools {
		key, p := entry.key, entry.value
		p.mu.Lock()
		m.mu.Lock()
		idle := p.refs == 0 && p.inflight == 0
		if idle && m.pools[key] == p {
			delete(m.pools, key)
			m.detached = append(m.detached, p)
		}
		m.mu.Unlock()
		if p.reserved && (idle || p.upload != p.savedUpload || p.download != p.savedDownload) {
			scratch.checkpoints = append(scratch.checkpoints, UsageCheckpoint{p.id, p.upload, p.download, idle})
			scratch.checkpointPools = append(scratch.checkpointPools, p)
		}
		p.mu.Unlock()
	}
	checkpoints, checkpointPools := scratch.checkpoints, scratch.checkpointPools
	result = m.store.CheckpointBatch(ctx, checkpoints)
	if result == nil {
		for i, point := range checkpoints {
			p := checkpointPools[i]
			p.mu.Lock()
			p.savedUpload, p.savedDownload = point.Upload, point.Download
			if point.Close {
				p.reserved = false
			}
			p.mu.Unlock()
		}
	}
	m.mu.Lock()
	clear(authorizations)
	used := authorizations
	for h := range m.handles {
		used[h.auth] = true
	}
	for key, a := range m.auth {
		if !used[a] {
			delete(m.auth, key)
		}
	}
	retired := m.poolSequence
	for _, p := range m.pools {
		if p.sequence <= retired {
			retired = p.sequence - 1
		}
	}
	compact := retired > m.retired
	m.mu.Unlock()
	if compact && result == nil {
		result = m.store.compactScope(ctx, m.epoch, retired, false)
		if result == nil {
			m.retired = retired
		}
	}
	if result != nil {
		m.Shutdown()
	} else {
		m.detached = nil
	}
	return result
}
func (m *Meter) Run(ctx context.Context) error {
	ticker := time.NewTicker(AuthorizationInterval)
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

// Stop admitting work before waiting for outstanding socket I/O to settle.
func (m *Meter) Shutdown() {
	m.mu.Lock()
	m.closed = true
	handles := make([]*Handle, 0, len(m.handles))
	for h := range m.handles {
		h.denied.Store(true)
		handles = append(handles, h)
	}
	pools := make([]*pool, 0, len(m.pools))
	for _, p := range m.pools {
		pools = append(pools, p)
	}
	m.mu.Unlock()
	for _, p := range pools {
		p.mu.Lock()
		close(p.changed)
		p.changed = make(chan struct{})
		p.mu.Unlock()
	}
	for _, h := range handles {
		_ = h.closer.Close()
	}
}
func (m *Meter) Close(ctx context.Context) error {
	m.Shutdown()
	m.maintenance.Lock()
	defer m.maintenance.Unlock()
	done := make(chan struct{})
	go func() { m.pending.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	pools := append([]*pool(nil), m.detached...)
	for _, p := range m.pools {
		pools = append(pools, p)
	}
	m.mu.Unlock()
	var result error
	for _, p := range pools {
		p.mu.Lock()
		if p.reserved {
			err := m.store.Checkpoint(ctx, p.id, p.upload, p.download, true)
			result = errors.Join(result, err)
			if err == nil {
				p.reserved = false
			}
		}
		p.mu.Unlock()
	}
	if result == nil {
		result = m.store.compactScope(ctx, m.epoch, m.poolSequence, true)
	}
	return result
}
