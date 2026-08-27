package jobs

import (
	"context"
	"fmt"
	"log"
	"time"
)

type Worker struct {
	repo        *Repository
	handlers    map[string]Handler
	wake        *Wake
	clock       Clock
	poll        time.Duration
	drain       int
	maxAttempts int
	report      func(error)
	retry       time.Duration
}

func NewWorker(repo *Repository, handlers map[string]Handler, wake *Wake, clock Clock) *Worker {
	return &Worker{repo: repo, handlers: handlers, wake: wake, clock: clock, poll: DefaultPollInterval, drain: DefaultDrainLimit, maxAttempts: DefaultMaxAttempts, report: func(err error) { log.Printf("background worker: %v", err) }, retry: time.Second}
}

func (w *Worker) SetErrorReporter(report func(error)) {
	if report != nil {
		w.report = report
	}
}

func safeReport(report func(error), err error) {
	if report == nil || err == nil {
		return
	}
	defer func() { _ = recover() }()
	report(err)
}

func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.repo.RecoverStuck(ctx, w.clock.Now(), DefaultStuckTimeout, w.maxAttempts, 200); err != nil {
		safeReport(w.report, fmt.Errorf("recover stuck jobs: %w", err))
	}
	w.wake.Signal()
	timer := time.NewTimer(w.poll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.wake.C():
		case <-timer.C:
		}
		more, err := w.drainDue(ctx)
		if err != nil && ctx.Err() == nil {
			safeReport(w.report, fmt.Errorf("drain due jobs: %w", err))
		}
		if more {
			w.wake.Signal()
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(w.poll)
	}
}

func (w *Worker) drainDue(ctx context.Context) (bool, error) {
	for i := 0; i < w.drain; i++ {
		if ctx.Err() != nil {
			return false, nil
		}
		job, ok, err := w.repo.Claim(ctx, w.clock.Now())
		if err != nil || !ok {
			return false, err
		}
		if err = w.execute(ctx, job); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (w *Worker) execute(ctx context.Context, job Job) error {
	var handlerErr error
	func() {
		defer func() {
			if recover() != nil {
				handlerErr = Retryable("JOB_HANDLER_PANIC", "job handler panicked")
			}
		}()
		handler, ok := w.handlers[job.Type]
		if !ok {
			handlerErr = Permanent("JOB_TYPE_UNSUPPORTED", "job type is unsupported")
			return
		}
		handlerErr = handler.Handle(ctx, job)
	}()
	now := w.clock.Now()
	if handlerErr == nil {
		return w.persist(ctx, "complete", func(persistCtx context.Context) error {
			return w.repo.Complete(persistCtx, job.ID, now)
		})
	}
	code, summary, permanent := classifyError(handlerErr)
	return w.persist(ctx, "fail", func(persistCtx context.Context) error {
		return w.repo.Fail(persistCtx, job, code, summary, permanent, now, w.maxAttempts)
	})
}

func (w *Worker) persist(ctx context.Context, transition string, fn func(context.Context) error) error {
	for {
		if err := fn(ctx); err == nil {
			return nil
		} else if ctx.Err() != nil {
			return nil
		} else {
			safeReport(w.report, fmt.Errorf("persist job %s: %w", transition, err))
		}
		timer := time.NewTimer(w.retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
