package accounting

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const reservationBatchSize = 64

type reservationResult struct {
	bytes int64
	err   error
}
type reservationRequest struct {
	ctx                  context.Context
	id, account, runtime string
	want, period         int64
	now                  time.Time
	result               chan reservationResult
}
type reservationWriter struct {
	requests chan *reservationRequest
	done     chan struct{}
}

// Coalesce requests already waiting on disk I/O, without a batching timer.
// Limits and lease checks remain per pool; no bytes are returned before FULL commit.
func (s *Store) reservePeriod(ctx context.Context, id, account, runtime string, want int64, now time.Time, periodID int64) (int64, error) {
	if want < 1 || want > ReservationBytes {
		return 0, errors.New("invalid reservation size")
	}
	r := &reservationRequest{ctx: ctx, id: id, account: account, runtime: runtime, want: want, period: periodID, now: now, result: make(chan reservationResult, 1)}
	s.reservationMu.Lock()
	if s.closed {
		s.reservationMu.Unlock()
		return 0, sql.ErrConnDone
	}
	if s.reservations == nil {
		s.reservations = &reservationWriter{requests: make(chan *reservationRequest, reservationBatchSize), done: make(chan struct{})}
		go s.writeReservations(s.reservations)
	}
	select {
	case s.reservations.requests <- r:
		s.reservationMu.Unlock()
	case <-ctx.Done():
		s.reservationMu.Unlock()
		return 0, ctx.Err()
	}
	// Once admitted, wait for the definitive commit result, including cancellation.
	result := <-r.result
	return result.bytes, result.err
}

func (s *Store) writeReservations(writer *reservationWriter) {
	defer close(writer.done)
	batch := make([]*reservationRequest, 0, reservationBatchSize)
	results := make([]reservationResult, reservationBatchSize)
	for first := range writer.requests {
		batch = append(batch, first)
	collect:
		for len(batch) < reservationBatchSize {
			select {
			case next, ok := <-writer.requests:
				if !ok {
					break collect
				}
				batch = append(batch, next)
			default:
				break collect
			}
		}
		s.commitReservations(batch, results[:len(batch)])
		clear(results[:len(batch)])
		clear(batch)
		batch = batch[:0]
	}
}

func (s *Store) commitReservations(batch []*reservationRequest, results []reservationResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err == nil {
		defer tx.Rollback()
		for i, r := range batch {
			if results[i].err = r.ctx.Err(); results[i].err != nil {
				continue
			}
			if len(batch) > 1 {
				if _, err = tx.ExecContext(ctx, "SAVEPOINT reservation"); err != nil {
					break
				}
			}
			results[i].bytes, results[i].err = reserveInTransaction(ctx, tx, r.id, r.account, r.runtime, r.want, r.now, r.period)
			if len(batch) == 1 && results[i].err != nil {
				err = results[i].err
				break
			}
			if len(batch) > 1 {
				if results[i].err != nil {
					if _, err = tx.ExecContext(ctx, "ROLLBACK TO reservation"); err != nil {
						break
					}
				}
				if _, err = tx.ExecContext(ctx, "RELEASE reservation"); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = tx.Commit()
		}
	}
	for i, r := range batch {
		if err != nil {
			results[i] = reservationResult{err: err}
		}
		r.result <- results[i]
	}
}
