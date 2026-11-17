package timetracking

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func guardedService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return (&Service{log: zap.NewNop()}).WithStopGuard(rdb), mr
}

// A stop replayed with the same key logs once; the replay is reported and logs nothing.
func TestStopGuardReplayLogsOnce(t *testing.T) {
	svc, _ := guardedService(t)
	ctx := context.Background()
	tenant, user := uuid.New(), uuid.New()
	calls := 0
	logOnce := func() error { calls++; return nil }

	if replayed, err := svc.guardedStop(ctx, tenant, user, "k1", logOnce); err != nil || replayed {
		t.Fatalf("first stop: replayed=%v err=%v", replayed, err)
	}
	if replayed, err := svc.guardedStop(ctx, tenant, user, "k1", logOnce); err != nil || !replayed {
		t.Fatalf("replay: replayed=%v err=%v, want a replay", replayed, err)
	}
	if calls != 1 {
		t.Fatalf("logged %d times, want once", calls)
	}
	// The same key from another user (or tenant) is a different stop.
	if replayed, _ := svc.guardedStop(ctx, tenant, uuid.New(), "k1", logOnce); replayed || calls != 2 {
		t.Fatalf("another user's key: replayed=%v calls=%d", replayed, calls)
	}
}

// A failed stop leaves no done marker and releases its lock, so the retry logs.
func TestStopGuardFailureCanBeRetried(t *testing.T) {
	svc, _ := guardedService(t)
	ctx := context.Background()
	tenant, user := uuid.New(), uuid.New()
	boom := errors.New("erp down")
	if _, err := svc.guardedStop(ctx, tenant, user, "k2", func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("failed stop: %v, want the log error", err)
	}
	calls := 0
	if replayed, err := svc.guardedStop(ctx, tenant, user, "k2", func() error { calls++; return nil }); err != nil || replayed || calls != 1 {
		t.Fatalf("retry: replayed=%v err=%v calls=%d, want a fresh log", replayed, err, calls)
	}
}

// While a stop with a key is logging, a duplicate is told to retry, never that it is done.
func TestStopGuardConcurrentDuplicateWaits(t *testing.T) {
	svc, _ := guardedService(t)
	ctx := context.Background()
	tenant, user := uuid.New(), uuid.New()
	_, err := svc.guardedStop(ctx, tenant, user, "k3", func() error {
		_, inner := svc.guardedStop(ctx, tenant, user, "k3", func() error { t.Fatal("duplicate logged"); return nil })
		if !errors.Is(inner, ErrStopInProgress) {
			t.Fatalf("duplicate during the first stop: %v, want ErrStopInProgress", inner)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// No key (or no Redis) means no guard: every call logs.
func TestStopGuardWithoutKeyAlwaysLogs(t *testing.T) {
	svc, _ := guardedService(t)
	calls := 0
	for i := 0; i < 2; i++ {
		if _, err := svc.guardedStop(context.Background(), uuid.New(), uuid.New(), "  ", func() error { calls++; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	unguarded := &Service{log: zap.NewNop()}
	if _, err := unguarded.guardedStop(context.Background(), uuid.New(), uuid.New(), "k", func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

// With Redis unreachable a keyed stop is refused rather than risking the same hours twice.
func TestStopGuardRedisDownRefuses(t *testing.T) {
	svc, mr := guardedService(t)
	mr.Close()
	if _, err := svc.guardedStop(context.Background(), uuid.New(), uuid.New(), "k4", func() error { t.Fatal("logged without a guard"); return nil }); !errors.Is(err, ErrGuardUnavailable) {
		t.Fatalf("redis down: %v, want ErrGuardUnavailable", err)
	}
}
