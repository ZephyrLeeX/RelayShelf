package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"

	"github.com/google/uuid"
)

// TOTPCipher encrypts TOTP shared secrets at rest. It reuses the deployment
// APP_ENCRYPTION_KEY but derives a domain-separated subkey with HKDF-SHA256,
// so TOTP ciphertexts are cryptographically independent from sensitive
// message-body ciphertexts and can never be confused with them.
const TOTPEncryptionVersion1 int16 = 1

var totpHKDFInfo = []byte("relayshelf-totp-secret-v1")
var totpAADDomain = []byte("relayshelf-totp-aad-v1")

type TOTPCipher struct{ key [32]byte }

func NewTOTPCipher(deploymentKey []byte) (*TOTPCipher, error) {
	if len(deploymentKey) != 32 {
		return nil, ErrCryptoKey
	}
	// HKDF-Extract with a fixed application salt, then HKDF-Expand to 32 bytes.
	hmacExtract := hmac.New(sha256.New, []byte("relayshelf-hkdf-totp-salt"))
	_, _ = hmacExtract.Write(deploymentKey)
	prk := hmacExtract.Sum(nil)
	hmacExpand := hmac.New(sha256.New, prk)
	_, _ = hmacExpand.Write(totpHKDFInfo)
	_, _ = hmacExpand.Write([]byte{0x01})
	okm := hmacExpand.Sum(nil)[:32]
	c := &TOTPCipher{}
	copy(c.key[:], okm)
	return c, nil
}

func totpAAD(version int16, userID uuid.UUID) []byte {
	out := make([]byte, 0, len(totpAADDomain)+2+16)
	out = append(out, totpAADDomain...)
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], uint16(version))
	out = append(out, encoded[:]...)
	out = append(out, userID[:]...)
	return out
}

func (c *TOTPCipher) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, ErrCryptoKey
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCryptoKey
	}
	return gcm, nil
}

// Encrypt seals a TOTP shared secret for one user with a fresh nonce.
func (c *TOTPCipher) Encrypt(userID uuid.UUID, secret []byte) (ciphertext, nonce []byte, version int16, err error) {
	gcm, err := c.aead()
	if err != nil {
		return nil, nil, 0, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, err
	}
	return gcm.Seal(nil, nonce, secret, totpAAD(TOTPEncryptionVersion1, userID)), nonce, TOTPEncryptionVersion1, nil
}

// Decrypt opens a stored TOTP secret. AAD binding means ciphertexts are not
// portable across users or versions.
func (c *TOTPCipher) Decrypt(userID uuid.UUID, version int16, nonce, ciphertext []byte) ([]byte, error) {
	if version != TOTPEncryptionVersion1 {
		return nil, ErrCryptoKey
	}
	gcm, err := c.aead()
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, ErrCryptoKey
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, totpAAD(version, userID))
	if err != nil {
		return nil, ErrCryptoKey
	}
	return plain, nil
}
