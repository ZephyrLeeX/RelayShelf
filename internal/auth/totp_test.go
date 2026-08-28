package auth

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// RFC 6238 Appendix B test vectors use the ASCII seed "12345678901234567890"
// (the base32 seed "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") with 8 digits. Our
// interoperable profile is 6 digits, so the expected values are the RFC
// truncated values modulo 10^6, computed by the same dynamic truncation.
func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := map[int64]string{
		59:          "94287082",
		1111111109:  "07081804",
		1111111111:  "14050471",
		1234567890:  "89005924",
		2000000000:  "69279037",
		20000000000: "65353130",
	}
	for timestamp, expected := range vectors {
		if got := TOTPCode(secret, timestamp/30, 8); got != expected {
			t.Fatalf("t=%d got %s want %s", timestamp, got, expected)
		}
	}
	sixDigitVectors := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for timestamp, expected := range sixDigitVectors {
		if got := TOTPCode(secret, timestamp/30, 6); got != expected {
			t.Fatalf("6-digit t=%d got %s want %s", timestamp, got, expected)
		}
	}
}

func TestValidateTOTPAcceptsDriftAndRejectsReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := int64(1111111109)
	code := TOTPCode(secret, now/30, 6)

	if step, err := ValidateTOTP(secret, code, now, -1); err != nil || step != now/30 {
		t.Fatalf("exact step err=%v step=%d", err, step)
	}
	// One step of drift in either direction still validates.
	if _, err := ValidateTOTP(secret, code, now+30, -1); err != nil {
		t.Fatalf("future drift rejected: %v", err)
	}
	if _, err := ValidateTOTP(secret, code, now-30, -1); err != nil {
		t.Fatalf("past drift rejected: %v", err)
	}
	// Two steps of drift is outside the window.
	if _, err := ValidateTOTP(secret, code, now+60, -1); err == nil {
		t.Fatal("two-step drift accepted")
	}
	// Replay: the accepted step and anything older must be refused.
	if _, err := ValidateTOTP(secret, code, now+30, now/30); err == nil {
		t.Fatal("replayed step accepted")
	}
	if _, err := ValidateTOTP(secret, TOTPCode(secret, now/30-1, 6), now+30, now/30); err == nil {
		t.Fatal("older step accepted after newer one")
	}
	// Malformed codes never validate.
	for _, bad := range []string{"", "12345", "1234567", "12345a", "abcdef", "12 456"} {
		if _, err := ValidateTOTP(secret, bad, now, -1); err == nil {
			t.Fatalf("malformed code %q accepted", bad)
		}
	}
}

func TestTOTPSecretEncodingRoundTrip(t *testing.T) {
	raw, encoded, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != TOTPSecretBytes || len(encoded) != 32 {
		t.Fatalf("raw=%d encoded=%d", len(raw), len(encoded))
	}
	if strings.Contains(encoded, "=") {
		t.Fatalf("encoded secret must be unpadded base32: %q", encoded)
	}
	decoded, err := DecodeTOTPSecret(encoded)
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("roundtrip err=%v", err)
	}
	if _, err = DecodeTOTPSecret("not!!base32@@"); err == nil {
		t.Fatal("invalid base32 accepted")
	}
	// Case-insensitive decoding matches authenticator conventions.
	lower, err := DecodeTOTPSecret(strings.ToLower(encoded))
	if err != nil || string(lower) != string(raw) {
		t.Fatalf("lowercase roundtrip err=%v", err)
	}
}

func TestTOTPOtpauthURLShape(t *testing.T) {
	url := TOTPOtpauthURL("alice@example", "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if !strings.HasPrefix(url, "otpauth://totp/RelayShelf%3Aalice@example?") {
		t.Fatalf("url=%q", url)
	}
	for _, required := range []string{"issuer=RelayShelf", "algorithm=SHA1", "digits=6", "period=30", "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"} {
		if !strings.Contains(url, required) {
			t.Fatalf("url=%q missing %s", url, required)
		}
	}
}

func TestTOTPCipherDomainSeparation(t *testing.T) {
	deploymentKey := make([]byte, 32)
	for i := range deploymentKey {
		deploymentKey[i] = byte(i + 7)
	}
	cipher, err := NewTOTPCipher(deploymentKey)
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.Must(uuid.NewV7())
	secret := []byte("12345678901234567890")
	ciphertext, nonce, version, err := cipher.Encrypt(user, secret)
	if err != nil {
		t.Fatal(err)
	}
	if version != TOTPEncryptionVersion1 {
		t.Fatalf("version=%d", version)
	}
	plain, err := cipher.Decrypt(user, version, nonce, ciphertext)
	if err != nil || string(plain) != string(secret) {
		t.Fatalf("decrypt err=%v", err)
	}

	// Ciphertext must be non-deterministic: fresh nonce per encryption.
	ciphertext2, nonce2, _, err := cipher.Encrypt(user, secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(nonce) == string(nonce2) || string(ciphertext) == string(ciphertext2) {
		t.Fatal("encryption reused nonce or produced identical ciphertext")
	}

	// AAD binds the user: another user cannot decrypt the same ciphertext.
	other := uuid.Must(uuid.NewV7())
	if _, err = cipher.Decrypt(other, version, nonce, ciphertext); err == nil {
		t.Fatal("cross-user decrypt accepted")
	}

	// The TOTP subkey must differ from the raw deployment key.
	if string(cipher.key[:]) == string(deploymentKey) {
		t.Fatal("TOTP subkey equals deployment key; domain separation missing")
	}

	if _, err = NewTOTPCipher(make([]byte, 31)); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestBase32StandardAlphabet(t *testing.T) {
	// The seed used by RFC vectors must decode through our decoder.
	raw, err := DecodeTOTPSecret("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "12345678901234567890" {
		t.Fatalf("rfc seed decoded to %q", raw)
	}
	if got := totpEncoding.EncodeToString(raw); got != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Fatalf("encoded %q", got)
	}
}
