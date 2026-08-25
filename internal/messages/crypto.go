package messages

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"

	"github.com/google/uuid"
)

const EncryptionVersion1 int16 = 1

var aadDomain = []byte("relayshelf-message-body-v1")

type BodyCipher interface {
	Encrypt(messageID, ownerID uuid.UUID, plaintext []byte) (ciphertext, nonce []byte, version int16, err error)
	Decrypt(message Message) ([]byte, error)
}

type AESGCMCipher struct{ key [32]byte }

func NewAESGCMCipher(key []byte) (*AESGCMCipher, error) {
	if len(key) != 32 {
		return nil, ErrCrypto
	}
	c := &AESGCMCipher{}
	copy(c.key[:], key)
	return c, nil
}

func aad(version int16, messageID, ownerID uuid.UUID) []byte {
	out := make([]byte, 0, len(aadDomain)+2+32)
	out = append(out, aadDomain...)
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], uint16(version))
	out = append(out, encoded[:]...)
	out = append(out, messageID[:]...)
	out = append(out, ownerID[:]...)
	return out
}

func (c *AESGCMCipher) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, ErrCrypto
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCrypto
	}
	return gcm, nil
}

func (c *AESGCMCipher) Encrypt(messageID, ownerID uuid.UUID, plaintext []byte) ([]byte, []byte, int16, error) {
	gcm, err := c.aead()
	if err != nil {
		return nil, nil, 0, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, 0, ErrCrypto
	}
	return gcm.Seal(nil, nonce, plaintext, aad(EncryptionVersion1, messageID, ownerID)), nonce, EncryptionVersion1, nil
}

func (c *AESGCMCipher) Decrypt(message Message) ([]byte, error) {
	if !message.Sensitive || message.BodyEncryptionVersion == nil || *message.BodyEncryptionVersion != EncryptionVersion1 {
		return nil, ErrCrypto
	}
	gcm, err := c.aead()
	if err != nil || len(message.BodyNonce) != gcm.NonceSize() {
		return nil, ErrCrypto
	}
	plain, err := gcm.Open(nil, message.BodyNonce, message.BodyCiphertext, aad(*message.BodyEncryptionVersion, message.ID, message.OwnerID))
	if err != nil {
		return nil, ErrCrypto
	}
	return plain, nil
}
