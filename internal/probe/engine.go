package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

var (
	ErrInvalid = errors.New("invalid check request")
	ErrBusy    = errors.New("too many active batches")
	idPattern  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,48}$`)
)

type Target struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
}
type Result struct {
	TargetID   string `json:"target_id"`
	Protocol   string `json:"protocol"`
	Up         bool   `json:"up"`
	LatencyMS  int64  `json:"latency_ms"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}
type Stats struct {
	Checks    int64   `json:"checks"`
	Successes int64   `json:"successes"`
	Failures  int64   `json:"failures"`
	Last      *Result `json:"last,omitempty"`
}
type Options struct {
	Concurrency int
	MaxBatches  int
	Timeout     time.Duration
}
type Checker func(context.Context, Target) Result
type Engine struct {
	targets []Target
	byID    map[string]Target
	slots   chan struct{}
	batches chan struct{}
	timeout time.Duration
	checker Checker
	mu      sync.RWMutex
	stats   map[string]Stats
}

func New(targets []Target, options Options, checker Checker) (*Engine, error) {
	if len(targets) == 0 || len(targets) > 32 || options.Concurrency < 1 || options.Concurrency > 32 || options.MaxBatches < 1 || options.MaxBatches > 32 || options.Timeout < 10*time.Millisecond || options.Timeout > 10*time.Second {
		return nil, fmt.Errorf("invalid engine configuration")
	}
	e := &Engine{targets: append([]Target(nil), targets...), byID: make(map[string]Target), slots: make(chan struct{}, options.Concurrency), batches: make(chan struct{}, options.MaxBatches), timeout: options.Timeout, checker: checker, stats: make(map[string]Stats)}
	if e.checker == nil {
		e.checker = NetworkChecker()
	}
	for _, target := range targets {
		if !idPattern.MatchString(target.ID) {
			return nil, fmt.Errorf("invalid target ID %q", target.ID)
		}
		if _, exists := e.byID[target.ID]; exists {
			return nil, fmt.Errorf("duplicate target %q", target.ID)
		}
		switch target.Protocol {
		case "http":
			u, err := url.Parse(target.Address)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
				return nil, fmt.Errorf("invalid HTTP address for %s", target.ID)
			}
		case "tcp":
			host, port, err := net.SplitHostPort(target.Address)
			number, parseErr := strconv.Atoi(port)
			if err != nil || parseErr != nil || host == "" || number < 1 || number > 65535 {
				return nil, fmt.Errorf("invalid TCP address for %s", target.ID)
			}
		default:
			return nil, fmt.Errorf("unknown protocol for %s", target.ID)
		}
		e.byID[target.ID] = target
		e.stats[target.ID] = Stats{}
	}
	return e, nil
}
func (e *Engine) Targets() []Target { return append([]Target(nil), e.targets...) }
func (e *Engine) Stats() map[string]Stats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	copy := make(map[string]Stats, len(e.stats))
	for id, stat := range e.stats {
		if stat.Last != nil {
			last := *stat.Last
			stat.Last = &last
		}
		copy[id] = stat
	}
	return copy
}
func (e *Engine) record(result Result) {
	e.mu.Lock()
	defer e.mu.Unlock()
	stat := e.stats[result.TargetID]
	stat.Checks++
	if result.Up {
		stat.Successes++
	} else {
		stat.Failures++
	}
	stat.Last = &result
	e.stats[result.TargetID] = stat
}
func (e *Engine) selectTargets(ids []string) ([]Target, error) {
	if len(ids) == 0 {
		return e.Targets(), nil
	}
	if len(ids) > 32 {
		return nil, fmt.Errorf("%w: maximum 32 target IDs", ErrInvalid)
	}
	seen := make(map[string]bool)
	targets := make([]Target, 0, len(ids))
	for _, id := range ids {
		target, ok := e.byID[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("%w: unknown or duplicate target ID %q", ErrInvalid, id)
		}
		seen[id] = true
		targets = append(targets, target)
	}
	return targets, nil
}

type job struct {
	index  int
	target Target
}

// Each request has a small worker pool. The shared slots channel limits network
// operations across ALL REST and gRPC requests, not just within one request.
func (e *Engine) Check(ctx context.Context, ids []string) ([]Result, error) {
	targets, err := e.selectTargets(ids)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case e.batches <- struct{}{}:
		defer func() { <-e.batches }()
	default:
		return nil, ErrBusy
	}
	jobs := make(chan job, len(targets))
	results := make([]Result, len(targets))
	for i, t := range targets {
		jobs <- job{i, t}
	}
	close(jobs)
	var wg sync.WaitGroup
	for range min(cap(e.slots), len(targets)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				select {
				case e.slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					<-e.slots
					return
				}
				checkCtx, cancel := context.WithTimeout(ctx, e.timeout)
				start := time.Now()
				result := e.checker(checkCtx, job.target)
				cancel()
				result.TargetID = job.target.ID
				result.Protocol = job.target.Protocol
				result.LatencyMS = time.Since(start).Milliseconds()
				<-e.slots
				results[job.index] = result
				e.record(result)
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
