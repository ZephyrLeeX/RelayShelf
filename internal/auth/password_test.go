package auth

import "testing"

func testHasher() *Argon2idHasher {
	return NewPasswordHasher(Argon2Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
}

func TestArgon2idHashVerify(t *testing.T) {
	h := testHasher()
	a, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("random salts produced identical encodings")
	}
	ok, needs, err := h.Verify(a, "correct horse battery staple")
	if err != nil || !ok || needs {
		t.Fatalf("ok=%v needs=%v err=%v", ok, needs, err)
	}
	ok, _, err = h.Verify(a, "wrong password")
	if err != nil || ok {
		t.Fatalf("wrong password ok=%v err=%v", ok, err)
	}
	if _, _, err = h.Verify("malformed", "x"); err == nil {
		t.Fatal("malformed hash accepted")
	}
	newer := NewPasswordHasher(Argon2Params{Memory: 128, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})
	ok, needs, err = newer.Verify(a, "correct horse battery staple")
	if err != nil || !ok || !needs {
		t.Fatalf("rehash ok=%v needs=%v err=%v", ok, needs, err)
	}
}

func TestDummyHashIsValid(t *testing.T) {
	if _, _, err := testHasher().Verify(DummyHash, "attacker-supplied-password"); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkArgon2(b *testing.B) {
	h := NewPasswordHasher(DefaultArgon2Params)
	encoded, err := h.Hash("benchmark-password")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := h.Verify(encoded, "benchmark-password"); err != nil {
			b.Fatal(err)
		}
	}
}
