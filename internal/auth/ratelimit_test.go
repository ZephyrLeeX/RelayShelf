package auth

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestRateLimiterDimensionsExpiryAndBound(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0)}
	limiter := NewRateLimiter(clock, 8)
	for i := 0; i < 3; i++ {
		limiter.Failure("192.0.2.1", "alice")
	}
	if limiter.Allow("192.0.2.1", "other") {
		t.Fatal("IP dimension did not block")
	}
	if limiter.Allow("192.0.2.2", "alice") {
		t.Fatal("username dimension did not block")
	}
	clock.now = clock.now.Add(2 * time.Second)
	if !limiter.Allow("192.0.2.1", "alice") {
		t.Fatal("temporary block did not expire")
	}
	for i := 0; i < 20; i++ {
		limiter.Failure(fmt.Sprintf("192.0.2.%d", i), fmt.Sprintf("user%d", i))
	}
	if limiter.Size() > 8 {
		t.Fatalf("state grew to %d", limiter.Size())
	}
	clock.now = clock.now.Add(RateEntryTTL + time.Second)
	_ = limiter.Allow("x", "x")
	if limiter.Size() != 0 {
		t.Fatalf("expired state remains: %d", limiter.Size())
	}
}

func TestRateLimiterChallengePressureAndSuccess(t *testing.T) {
	clock := &fakeClock{now: time.Unix(2000, 0)}
	limiter := NewRateLimiter(clock, 16)
	for range 3 {
		if !limiter.AllowChallenge("192.0.2.10", "alice") {
			t.Fatal("challenge capacity rejected too early")
		}
	}
	if limiter.AllowChallenge("192.0.2.10", "alice") {
		t.Fatal("challenge capacity did not reject the bounded request")
	}
	if limiter.Allow("192.0.2.10", "other") {
		t.Fatal("challenge pressure did not block the IP dimension")
	}
	if limiter.Allow("192.0.2.11", "alice") {
		t.Fatal("challenge pressure did not block the username dimension")
	}
	limiter.Success("192.0.2.10", "alice")
	if !limiter.Allow("192.0.2.10", "alice") {
		t.Fatal("successful TOTP completion did not relax challenge pressure")
	}
}
