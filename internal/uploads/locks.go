package uploads

import (
	"sync"

	"github.com/google/uuid"
)

type uploadLock struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	parts     map[int]*partLock
	refs      int
}

type partLock struct {
	mu   sync.Mutex
	refs int
}

type LockRegistry struct {
	mu      sync.Mutex
	uploads map[uuid.UUID]*uploadLock
}

func NewLockRegistry() *LockRegistry { return &LockRegistry{uploads: make(map[uuid.UUID]*uploadLock)} }

func (r *LockRegistry) acquire(id uuid.UUID) *uploadLock {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.uploads[id]
	if l == nil {
		l = &uploadLock{parts: make(map[int]*partLock)}
		r.uploads[id] = l
	}
	l.refs++
	return l
}

func (r *LockRegistry) release(id uuid.UUID, l *uploadLock) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l.refs--
	if l.refs == 0 {
		delete(r.uploads, id)
	}
}

func (r *LockRegistry) Chunk(id uuid.UUID, part int) func() {
	l := r.acquire(id)
	l.lifecycle.RLock()
	l.mu.Lock()
	p := l.parts[part]
	if p == nil {
		p = &partLock{}
		l.parts[part] = p
	}
	p.refs++
	l.mu.Unlock()
	p.mu.Lock()
	return func() {
		p.mu.Unlock()
		l.mu.Lock()
		p.refs--
		if p.refs == 0 {
			delete(l.parts, part)
		}
		l.mu.Unlock()
		l.lifecycle.RUnlock()
		r.release(id, l)
	}
}

func (r *LockRegistry) Exclusive(id uuid.UUID) func() {
	l := r.acquire(id)
	l.lifecycle.Lock()
	return func() {
		l.lifecycle.Unlock()
		r.release(id, l)
	}
}

func (r *LockRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.uploads)
}

type WriteSemaphore struct{ slots chan struct{} }

func NewWriteSemaphore(limit int) *WriteSemaphore {
	if limit < 1 {
		panic("chunk write limit must be positive")
	}
	return &WriteSemaphore{slots: make(chan struct{}, limit)}
}

func (s *WriteSemaphore) Acquire(done <-chan struct{}) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-done:
		return ErrStagingUnavailable
	}
}

func (s *WriteSemaphore) Release() { <-s.slots }
