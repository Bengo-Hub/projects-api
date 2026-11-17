package timetracking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sharedcache "github.com/Bengo-Hub/cache"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// A timer stop adds hours to ERP (it is not a set), so a stop the browser sends twice (a lost
// reply, an offline queue replay, two devices) must log once. With an Idempotency-Key the stop is
// guarded fleet-wide in Redis:
//
//   - a key already marked done within stopDoneTTL is answered as a replay and logs nothing;
//   - otherwise a short lock on the key keeps a concurrent duplicate out (it gets
//     ErrStopInProgress and retries later, so it is never told "done" before anything logged);
//   - the done marker is written only after every date logged, and the lock is released either
//     way, so a failed or partial stop can be retried with the same key.
const (
	stopDoneTTL      = 24 * time.Hour
	stopLockTTL      = 2 * time.Minute
	maxStopKeyLength = 128
)

// ErrStopInProgress means another request with the same Idempotency-Key is logging this stop.
var ErrStopInProgress = errors.New("this timer stop is already being logged; try again shortly")

// ErrGuardUnavailable means the stop could not be guarded (Redis unreachable). Refusing is safer
// than risking the same hours twice; the browser retries.
var ErrGuardUnavailable = errors.New("time tracking cannot confirm this stop right now; try again shortly")

// WithStopGuard enables Idempotency-Key handling on timer stops. Without Redis (local runs) stops
// are not deduplicated.
func (s *Service) WithStopGuard(rdb redis.UniversalClient) *Service {
	s.rdb = rdb
	return s
}

func stopKeys(tenantID, userID uuid.UUID, key string) (done, lock string) {
	base := "projects:timer-stop:" + tenantID.String() + ":" + userID.String() + ":" + key
	return base + ":done", base + ":lock"
}

// guardedStop runs log under the stop guard for key. replayed reports a key already done.
func (s *Service) guardedStop(ctx context.Context, tenantID, userID uuid.UUID, key string, log func() error) (replayed bool, err error) {
	key = strings.TrimSpace(key)
	if key == "" || s.rdb == nil {
		return false, log()
	}
	if len(key) > maxStopKeyLength {
		return false, ValidationError(fmt.Sprintf("Idempotency-Key is at most %d characters", maxStopKeyLength))
	}
	done, lockKey := stopKeys(tenantID, userID, key)
	n, err := s.rdb.Exists(ctx, done).Result()
	if err != nil {
		return false, ErrGuardUnavailable
	}
	if n > 0 {
		return true, nil
	}
	lock, ok, err := sharedcache.TryLock(ctx, s.rdb, lockKey, stopLockTTL)
	if err != nil {
		return false, ErrGuardUnavailable
	}
	if !ok {
		return false, ErrStopInProgress
	}
	defer func() {
		if rerr := lock.Release(context.WithoutCancel(ctx)); rerr != nil {
			s.log.Warn("timer stop lock not released", zap.Error(rerr))
		}
	}()
	if err := log(); err != nil {
		return false, err
	}
	if err := s.rdb.Set(ctx, done, "1", stopDoneTTL).Err(); err != nil {
		// The hours are logged; only the replay marker is missing, so a replay could log again.
		s.log.Error("timer stop logged but its idempotency marker was not saved", zap.Error(err))
	}
	return false, nil
}
