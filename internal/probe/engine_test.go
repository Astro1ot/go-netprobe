package probe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func targets() []Target {
	return []Target{{"a", "tcp", "localhost:80"}, {"b", "tcp", "localhost:81"}, {"c", "tcp", "localhost:82"}}
}
func engine(t *testing.T, options Options, checker Checker) *Engine {
	t.Helper()
	e, err := New(targets(), options, checker)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestGlobalConcurrencyAndStableResultOrder(t *testing.T) {
	var active, maximum atomic.Int32
	checker := func(ctx context.Context, target Target) Result {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		timer := time.NewTimer(15 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Result{Error: ctx.Err().Error()}
		case <-timer.C:
			return Result{Up: true}
		}
	}
	e := engine(t, Options{Concurrency: 2, MaxBatches: 8, Timeout: time.Second}, checker)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := e.Check(context.Background(), []string{"c", "a", "b"})
			if err == nil && (len(results) != 3 || results[0].TargetID != "c" || results[1].TargetID != "a" || results[2].TargetID != "b") {
				err = fmt.Errorf("unstable result order: %v", results)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maximum.Load() > 2 || maximum.Load() == 0 {
		t.Fatalf("max simultaneous probes = %d", maximum.Load())
	}
	stats := e.Stats()
	for id, stat := range stats {
		if stat.Checks != 6 || stat.Successes != 6 {
			t.Fatalf("%s: %+v", id, stat)
		}
	}
	stats["a"].Last.Up = false
	if !e.Stats()["a"].Last.Up {
		t.Fatal("stats leaked mutable state")
	}
}
func TestBackpressureAndCancellationReleaseSlots(t *testing.T) {
	started := make(chan struct{}, 1)
	e := engine(t, Options{Concurrency: 1, MaxBatches: 1, Timeout: time.Second}, func(ctx context.Context, target Target) Result {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return Result{Error: ctx.Err().Error()}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := e.Check(ctx, []string{"a"}); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if _, err := e.Check(context.Background(), []string{"b"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected backpressure, got %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if len(e.slots) != 0 || len(e.batches) != 0 {
		t.Fatal("capacity leaked after cancellation")
	}
}
func TestPerTargetTimeoutAndInvalidIDs(t *testing.T) {
	e := engine(t, Options{Concurrency: 2, MaxBatches: 2, Timeout: 20 * time.Millisecond}, func(ctx context.Context, target Target) Result { <-ctx.Done(); return Result{Error: ctx.Err().Error()} })
	results, err := e.Check(context.Background(), []string{"a"})
	if err != nil || len(results) != 1 || results[0].Up || results[0].Error == "" {
		t.Fatalf("timeout result: %v %v", results, err)
	}
	for _, ids := range [][]string{{"unknown"}, {"a", "a"}} {
		if _, err := e.Check(context.Background(), ids); !errors.Is(err, ErrInvalid) {
			t.Fatalf("expected validation error: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Check(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled request was accepted")
	}
}
func TestConfigurationValidation(t *testing.T) {
	options := Options{Concurrency: 2, MaxBatches: 2, Timeout: time.Second}
	for _, input := range [][]Target{nil, {{"a", "tcp", "host"}}, {{"a", "tcp", "host:0"}}, {{"a", "http", "file:///tmp/test"}}, {{"a", "http", "http://user:pass@host"}}, {{"a", "tcp", "host:80"}, {"a", "tcp", "host:81"}}} {
		if _, err := New(input, options, nil); err == nil {
			t.Fatalf("accepted invalid configuration: %v", input)
		}
	}
}
