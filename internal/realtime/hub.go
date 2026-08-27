package realtime

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

const DefaultSubscriberBuffer = 64

type Subscriber struct {
	ID     uuid.UUID
	Events <-chan Event
	cancel context.CancelFunc
}

type subscription struct {
	id     uuid.UUID
	ch     chan Event
	cancel context.CancelFunc
}

type Hub struct {
	mu     sync.RWMutex
	users  map[uuid.UUID]map[uuid.UUID]*subscription
	buffer int
}

func NewHub() *Hub {
	return &Hub{users: make(map[uuid.UUID]map[uuid.UUID]*subscription), buffer: DefaultSubscriberBuffer}
}

func (h *Hub) Subscribe(parent context.Context, userID uuid.UUID) (context.Context, Subscriber, func()) {
	ctx, cancel := context.WithCancel(parent)
	id := uuid.New()
	sub := &subscription{id: id, ch: make(chan Event, h.buffer), cancel: cancel}
	h.mu.Lock()
	if h.users[userID] == nil {
		h.users[userID] = make(map[uuid.UUID]*subscription)
	}
	h.users[userID][id] = sub
	h.mu.Unlock()
	var once sync.Once
	unregister := func() {
		once.Do(func() {
			h.mu.Lock()
			if entries := h.users[userID]; entries != nil {
				delete(entries, id)
				if len(entries) == 0 {
					delete(h.users, userID)
				}
			}
			h.mu.Unlock()
			cancel()
		})
	}
	return ctx, Subscriber{ID: id, Events: sub.ch, cancel: cancel}, unregister
}

func (h *Hub) Publish(userID uuid.UUID, event Event) {
	var slow []context.CancelFunc
	h.mu.RLock()
	for _, sub := range h.users[userID] {
		select {
		case sub.ch <- event:
		default:
			slow = append(slow, sub.cancel)
		}
	}
	h.mu.RUnlock()
	for _, cancel := range slow {
		cancel()
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	var cancels []context.CancelFunc
	for _, entries := range h.users {
		for _, sub := range entries {
			cancels = append(cancels, sub.cancel)
		}
	}
	h.users = make(map[uuid.UUID]map[uuid.UUID]*subscription)
	h.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
