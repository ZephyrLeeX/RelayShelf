package realtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHubConcurrentSubscribePublishUnsubscribe(t *testing.T) {
	h := NewHub()
	user := uuid.New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				ctx, cancel := context.WithCancel(context.Background())
				_, sub, unregister := h.Subscribe(ctx, user)
				h.Publish(user, Event{ID: uuid.New(), Type: MessageUpdated, ResourceID: uuid.New()})
				select {
				case <-sub.Events:
				default:
				}
				cancel()
				unregister()
			}
		}()
	}
	wg.Wait()
	h.Close()
}

func TestHubSlowSubscriberDoesNotBlockOthers(t *testing.T) {
	h := NewHub()
	user := uuid.New()
	slowCtx, slowCancel := context.WithCancel(context.Background())
	defer slowCancel()
	slowStream, _, slowUnregister := h.Subscribe(slowCtx, user)
	defer slowUnregister()
	fastCtx, fastCancel := context.WithCancel(context.Background())
	defer fastCancel()
	_, fast, fastUnregister := h.Subscribe(fastCtx, user)
	defer fastUnregister()
	received := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-fastCtx.Done():
				return
			case <-fast.Events:
				select {
				case received <- struct{}{}:
				default:
				}
			}
		}
	}()
	for i := 0; i < DefaultSubscriberBuffer+20; i++ {
		done := make(chan struct{})
		go func() {
			h.Publish(user, Event{ID: uuid.New(), Type: MessageUpdated, ResourceID: uuid.New()})
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("publish blocked on slow subscriber")
		}
		select {
		case <-received:
		case <-time.After(time.Second):
			t.Fatal("fast subscriber received no event")
		}
	}
	select {
	case <-slowStream.Done():
	case <-time.After(time.Second):
		t.Fatal("slow subscriber was not canceled")
	}
}

func TestHubUserIsolation(t *testing.T) {
	h := NewHub()
	alice, bob := uuid.New(), uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, as, au := h.Subscribe(ctx, alice)
	defer au()
	_, bs, bu := h.Subscribe(ctx, bob)
	defer bu()
	ae := Event{ID: uuid.New(), Type: MessageCreated, ResourceID: uuid.New()}
	be := Event{ID: uuid.New(), Type: MessageCreated, ResourceID: uuid.New()}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); h.Publish(alice, ae) }()
	go func() { defer wg.Done(); h.Publish(bob, be) }()
	wg.Wait()
	if got := <-as.Events; got.ID != ae.ID {
		t.Fatal("alice received bob event")
	}
	if got := <-bs.Events; got.ID != be.ID {
		t.Fatal("bob received alice event")
	}
}
