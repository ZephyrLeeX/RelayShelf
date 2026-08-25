package messages

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestAESGCMRoundTripFreshNonceAndAAD(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	cipher, err := NewAESGCMCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	messageID, ownerID := uuid.New(), uuid.New()
	body := []byte("classified text")
	first, nonce, version, err := cipher.Encrypt(messageID, ownerID, body)
	if err != nil {
		t.Fatal(err)
	}
	second, nonce2, _, err := cipher.Encrypt(messageID, ownerID, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != 12 || len(nonce2) != 12 || bytes.Equal(nonce, nonce2) || bytes.Equal(first, second) {
		t.Fatal("encryption did not use fresh 12-byte nonces")
	}
	m := Message{ID: messageID, OwnerID: ownerID, Sensitive: true, BodyCiphertext: first, BodyNonce: nonce, BodyEncryptionVersion: &version}
	plain, err := cipher.Decrypt(m)
	if err != nil || !bytes.Equal(plain, body) {
		t.Fatalf("roundtrip failed: %v", err)
	}
	m.ID = uuid.New()
	if _, err = cipher.Decrypt(m); !errors.Is(err, ErrCrypto) {
		t.Fatal("wrong message AAD succeeded")
	}
	m.ID = messageID
	m.OwnerID = uuid.New()
	if _, err = cipher.Decrypt(m); !errors.Is(err, ErrCrypto) {
		t.Fatal("wrong owner AAD succeeded")
	}
}

func TestAESGCMTamperAndVersionFailSanitized(t *testing.T) {
	cipher, _ := NewAESGCMCipher(bytes.Repeat([]byte{9}, 32))
	id, owner := uuid.New(), uuid.New()
	sealed, nonce, version, _ := cipher.Encrypt(id, owner, []byte("do not print"))
	m := Message{ID: id, OwnerID: owner, Sensitive: true, BodyCiphertext: sealed, BodyNonce: nonce, BodyEncryptionVersion: &version}
	m.BodyCiphertext[0] ^= 1
	if _, err := cipher.Decrypt(m); !errors.Is(err, ErrCrypto) {
		t.Fatal("tampered ciphertext succeeded")
	}
	bad := int16(2)
	m.BodyEncryptionVersion = &bad
	if _, err := cipher.Decrypt(m); !errors.Is(err, ErrCrypto) {
		t.Fatal("unknown version succeeded")
	}
}
