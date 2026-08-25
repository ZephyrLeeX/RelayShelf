package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultArgon2Params = Argon2Params{Memory: 64 * 1024, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (ok bool, needsRehash bool, err error)
}

type Argon2idHasher struct{ Params Argon2Params }

func NewPasswordHasher(params Argon2Params) *Argon2idHasher { return &Argon2idHasher{Params: params} }

func (h *Argon2idHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.Params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.Params.Iterations, h.Params.Memory, h.Params.Parallelism, h.Params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.Params.Memory, h.Params.Iterations, h.Params.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *Argon2idHasher) Verify(encoded, password string) (bool, bool, error) {
	p, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, uint32(len(expected)))
	ok := subtle.ConstantTimeCompare(actual, expected) == 1
	needs := p.Memory != h.Params.Memory || p.Iterations != h.Params.Iterations || p.Parallelism != h.Params.Parallelism || uint32(len(salt)) != h.Params.SaltLength || uint32(len(expected)) != h.Params.KeyLength
	return ok, needs, nil
}

func parseArgon2id(encoded string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errors.New("invalid argon2id encoding")
	}
	var p Argon2Params
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, errors.New("unsupported argon2id version")
	}
	values := strings.Split(parts[3], ",")
	if len(values) != 3 || !strings.HasPrefix(values[0], "m=") || !strings.HasPrefix(values[1], "t=") || !strings.HasPrefix(values[2], "p=") {
		return p, nil, nil, errors.New("invalid argon2id parameters")
	}
	memory, memoryErr := strconv.ParseUint(strings.TrimPrefix(values[0], "m="), 10, 32)
	iterations, iterationsErr := strconv.ParseUint(strings.TrimPrefix(values[1], "t="), 10, 32)
	parallelism, parallelismErr := strconv.ParseUint(strings.TrimPrefix(values[2], "p="), 10, 8)
	p.Memory, p.Iterations, p.Parallelism = uint32(memory), uint32(iterations), uint8(parallelism)
	if memoryErr != nil || iterationsErr != nil || parallelismErr != nil || p.Memory < 8 || p.Memory > 1024*1024 || p.Iterations < 1 || p.Iterations > 20 || p.Parallelism < 1 || p.Parallelism > 32 {
		return p, nil, nil, errors.New("invalid argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return p, nil, nil, errors.New("invalid argon2id salt")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, errors.New("invalid argon2id key")
	}
	p.SaltLength, p.KeyLength = uint32(len(salt)), uint32(len(key))
	return p, salt, key, nil
}

// DummyHash is a syntactically valid, non-secret hash used for unknown users.
const DummyHash = "$argon2id$v=19$m=65536,t=2,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
