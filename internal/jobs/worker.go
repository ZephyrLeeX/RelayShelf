package jobs

import (
	"context"
	"fmt"
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
}

func NewWorker(repo *Repository, handlers map[string]Handler, wake *Wake, clock Clock) *Worker {
	return &Worker{repo: repo, handlers: handlers, wake: wake, clock: clock, poll: DefaultPollInterval, drain: DefaultDrainLimit, maxAttempts: DefaultMaxAttempts}
}

func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.repo.RecoverStuck(ctx, w.clock.Now(), DefaultStuckTimeout, w.maxAttempts, 200); err != nil {
		return fmt.Errorf("recover stuck jobs: %w", err)
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
			return err
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
		w.execute(ctx, job)
	}
	return true, nil
}

func (w *Worker) execute(ctx context.Context, job Job) {
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
		_ = w.repo.Complete(context.WithoutCancel(ctx), job.ID, now)
		return
	}
	code, summary, permanent := classifyError(handlerErr)
	_ = w.repo.Fail(context.WithoutCancel(ctx), job, code, summary, permanent, now, w.maxAttempts)
}
