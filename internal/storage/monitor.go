package storage

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Monitor tracks storage reachability with bounded probes and exposes a
// fast, lock-free health snapshot. It implements the Phase 11 NFS-outage
// boundary: once storage is known degraded, storage-heavy request paths can
// reject immediately instead of queueing behind hard-mount NFS syscalls that
// may block for the duration of an outage. Text, tag, search, and staging
// upload paths never consult it and keep working.
type Monitor struct {
	probe        func(context.Context) error
	interval     time.Duration
	failures     int
	failureCount atomic.Int32
	gate         chan struct{}
	healthy      atomic.Bool
	reason       atomic.Value // string
	snapshot     atomic.Value // HealthSnapshot
	startOnce    sync.Once
}

type HealthSnapshot struct {
	Healthy       bool
	Reason        string
	LastCheckedAt *time.Time
	ChangedAt     time.Time
}

// DefaultMonitorInterval is deliberately modest: probes are cheap statfs
// calls, and a stuck probe holds the gate so later probes never pile up.
const DefaultMonitorInterval = 15 * time.Second

// DefaultMonitorFailureThreshold requires two consecutive failed probes
// before declaring degradation, so a single scheduler blip does not flap the
// storage surface. One successful probe restores health.
const DefaultMonitorFailureThreshold = 2

func NewMonitor(adapter Adapter) *Monitor {
	return NewMonitorTunable(adapter, DefaultMonitorInterval, DefaultMonitorFailureThreshold)
}

// NewMonitorTunable configures the probe interval and consecutive-failure
// threshold; production uses the defaults, tests shrink both.
func NewMonitorTunable(adapter Adapter, interval time.Duration, failureThreshold int) *Monitor {
	m := &Monitor{interval: interval, failures: failureThreshold, gate: make(chan struct{}, 1)}
	m.probe = func(ctx context.Context) error {
		space, err := adapter.Space(ctx)
		if err == nil && space.AvailableBytes == 0 {
			return ErrFull
		}
		return err
	}
	m.healthy.Store(true)
	m.reason.Store("")
	now := time.Now().UTC()
	m.snapshot.Store(HealthSnapshot{Healthy: true, Reason: "HEALTHY", ChangedAt: now})
	return m
}

// NewMonitorWithProbe is the test seam: the probe stands in for Space so the
// monitor can be driven deterministically.
func NewMonitorWithProbe(probe func(context.Context) error, interval time.Duration, failureThreshold int) *Monitor {
	if interval <= 0 {
		interval = time.Millisecond
	}
	if failureThreshold < 1 {
		failureThreshold = 1
	}
	m := &Monitor{probe: probe, interval: interval, failures: failureThreshold, gate: make(chan struct{}, 1)}
	m.healthy.Store(true)
	m.reason.Store("")
	now := time.Now().UTC()
	m.snapshot.Store(HealthSnapshot{Healthy: true, Reason: "HEALTHY", ChangedAt: now})
	return m
}

func classifyCause(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrFull) {
		return "NAS_FULL"
	}
	return "NAS_UNAVAILABLE"
}

// Healthy reports the last settled storage state. A zero-value or never-run
// monitor reports healthy so wiring mistakes fail open into normal behavior
// rather than rejecting everything.
func (m *Monitor) Healthy() bool { return m == nil || m.healthy.Load() }

// Reason describes the last probe outcome for operational surfaces. It never
// contains paths or secrets, only classified causes.
func (m *Monitor) Reason() string {
	if m == nil {
		return ""
	}
	reason, _ := m.reason.Load().(string)
	return reason
}

// Snapshot returns only atomically maintained memory state. It never touches
// the filesystem and is therefore safe for request handlers even when a hard
// NFS mount has an indefinitely blocked syscall.
func (m *Monitor) Snapshot() HealthSnapshot {
	if m == nil {
		now := time.Now().UTC()
		return HealthSnapshot{Healthy: true, Reason: "HEALTHY", ChangedAt: now}
	}
	snapshot, _ := m.snapshot.Load().(HealthSnapshot)
	return snapshot
}

// Run probes until the context is cancelled. Run returns immediately if the
// monitor is nil.
func (m *Monitor) Run(ctx context.Context) {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			m.probeOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

// probeOnce runs one bounded probe. A probe that does not finish within the
// interval counts as a timeout, which is itself a degraded signal; the gate
// guarantees at most one probe syscall is ever outstanding.
func (m *Monitor) probeOnce(ctx context.Context) {
	select {
	case m.gate <- struct{}{}:
	default:
		// The previous probe is still stuck inside the filesystem; that is
		// the strongest possible degradation signal.
		m.settle(false, "NAS_TIMEOUT")
		return
	}
	done := make(chan error, 1)
	go func() {
		probeCtx, cancel := context.WithTimeout(context.Background(), m.interval)
		defer cancel()
		done <- m.probe(probeCtx)
		<-m.gate
	}()
	timer := time.NewTimer(m.interval)
	defer timer.Stop()
	select {
	case err := <-done:
		if err == nil {
			m.settle(true, "")
			return
		}
		m.settle(false, classifyCause(err))
	case <-timer.C:
		m.settle(false, "NAS_TIMEOUT")
	}
}

// settle applies the consecutive-failure policy: degraded after the
// configured threshold of consecutive failures, healthy after one success.
func (m *Monitor) settle(healthy bool, reason string) {
	now := time.Now().UTC()
	previous := m.Snapshot()
	if healthy {
		wasHealthy := m.healthy.Load()
		m.reason.Store("")
		m.healthy.Store(true)
		m.failureCount.Store(0)
		changedAt := previous.ChangedAt
		if !wasHealthy {
			changedAt = now
			log.Printf("storage health changed: degraded -> healthy")
		}
		m.snapshot.Store(HealthSnapshot{Healthy: true, Reason: "HEALTHY", LastCheckedAt: &now, ChangedAt: changedAt})
		return
	}
	if int(m.failureCount.Add(1)) >= m.failures {
		wasHealthy := m.healthy.Load()
		previousReason := m.Reason()
		m.reason.Store(reason)
		m.healthy.Store(false)
		changedAt := previous.ChangedAt
		if wasHealthy || previousReason != reason {
			changedAt = now
			log.Printf("storage health changed: %s -> degraded reason=%s", previous.Reason, reason)
		}
		m.snapshot.Store(HealthSnapshot{Healthy: false, Reason: reason, LastCheckedAt: &now, ChangedAt: changedAt})
		return
	}
	previous.LastCheckedAt = &now
	m.snapshot.Store(previous)
}
