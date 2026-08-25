package uploads

import (
	"testing"

	"github.com/google/uuid"
)

func TestLockRegistrySerializesSamePartAndCleansReferences(t *testing.T) {
	registry := NewLockRegistry()
	id := uuid.Must(uuid.NewV7())
	first := registry.Chunk(id, 0)
	acquired := make(chan func(), 1)
	go func() { acquired <- registry.Chunk(id, 0) }()
	select {
	case unlock := <-acquired:
		unlock()
		t.Fatal("same part did not serialize")
	default:
	}
	first()
	second := <-acquired
	second()
	if registry.Len() != 0 {
		t.Fatalf("lock registry leaked %d entries", registry.Len())
	}
}

func TestDifferentPartsRunTogetherAndExclusiveWaits(t *testing.T) {
	registry := NewLockRegistry()
	id := uuid.Must(uuid.NewV7())
	part0 := registry.Chunk(id, 0)
	part1Ready := make(chan func(), 1)
	go func() { part1Ready <- registry.Chunk(id, 1) }()
	part1 := <-part1Ready
	exclusiveReady := make(chan func(), 1)
	go func() { exclusiveReady <- registry.Exclusive(id) }()
	select {
	case unlock := <-exclusiveReady:
		unlock()
		t.Fatal("exclusive lock bypassed chunks")
	default:
	}
	part0()
	select {
	case unlock := <-exclusiveReady:
		unlock()
		t.Fatal("exclusive lock ignored second chunk")
	default:
	}
	part1()
	exclusive := <-exclusiveReady
	exclusive()
	if registry.Len() != 0 {
		t.Fatal("registry leaked")
	}
}

func TestContentFinalizeLockSerializesAndReclaimsEntry(t *testing.T) {
	registry := newContentLockRegistry()
	first := registry.lock("hash:size")
	acquired := make(chan func(), 1)
	go func() { acquired <- registry.lock("hash:size") }()
	select {
	case unlock := <-acquired:
		unlock()
		t.Fatal("identical content did not serialize")
	default:
	}
	first()
	second := <-acquired
	second()
	registry.mu.Lock()
	remaining := len(registry.locks)
	registry.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("content finalize registry leaked %d entries", remaining)
	}
}
