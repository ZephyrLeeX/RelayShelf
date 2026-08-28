package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Minimal RFC 6238 / RFC 4226 TOTP implementation (HMAC-SHA1, the interoperable
// authenticator profile). This is deliberately a small standard implementation
// pinned by RFC test vectors rather than a new dependency: validation accepts
// codes from the previous, current, and next time step, and a step is never
// accepted twice for the same enrollment (replay protection lives in the
// caller, which persists last_used_step).

const (
	TOTPDigits        = 6
	TOTPPeriodSeconds = 30
	// TOTPSkew allows one step of clock drift in either direction, matching
	// common authenticator defaults.
	TOTPSkew = 1
	// TOTPSecretBytes is 160 bits of CSPRNG entropy, the RFC 4226 minimum.
	TOTPSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrTOTPCodeInvalid is returned for any mismatching or malformed code. The
// error never distinguishes expired, replayed, or wrong codes.
var ErrTOTPCodeInvalid = errors.New("totp code invalid")

func GenerateTOTPSecret() (raw []byte, encoded string, err error) {
	raw = make([]byte, TOTPSecretBytes)
	if _, err = rand.Read(raw); err != nil {
		return nil, "", err
	}
	return raw, totpEncoding.EncodeToString(raw), nil
}

func DecodeTOTPSecret(encoded string) ([]byte, error) {
	upper := strings.ToUpper(strings.TrimSpace(encoded))
	raw, err := totpEncoding.DecodeString(upper)
	if err != nil || len(raw) == 0 {
		return nil, ErrTOTPCodeInvalid
	}
	return raw, nil
}

// TOTPCode derives the RFC 4226 dynamic truncation output for a counter step.
func TOTPCode(secret []byte, step int64, digits int) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (int64(sum[offset])&0x7f)<<24 |
		(int64(sum[offset+1])&0xff)<<16 |
		(int64(sum[offset+2])&0xff)<<8 |
		(int64(sum[offset+3]) & 0xff)
	modulo := int64(1)
	for i := 0; i < digits; i++ {
		modulo *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulo)
}

// ValidateTOTP checks code against secret for the time step containing now,
// with TOTPSkew steps of tolerance, rejecting any step <= lastUsedStep. It
// returns the accepted step so callers can persist replay protection.
func ValidateTOTP(secret []byte, code string, nowSeconds, lastUsedStep int64) (int64, error) {
	normalized := strings.TrimSpace(code)
	if len(normalized) != TOTPDigits {
		return 0, ErrTOTPCodeInvalid
	}
	for _, char := range normalized {
		if char < '0' || char > '9' {
			return 0, ErrTOTPCodeInvalid
		}
	}
	current := nowSeconds / TOTPPeriodSeconds
	var accepted int64 = -1
	for step := current - TOTPSkew; step <= current+TOTPSkew; step++ {
		if step < 0 {
			continue
		}
		if lastUsedStep >= 0 && step <= lastUsedStep {
			continue
		}
		if hmac.Equal([]byte(TOTPCode(secret, step, TOTPDigits)), []byte(normalized)) {
			accepted = step
			break
		}
	}
	if accepted < 0 {
		return 0, ErrTOTPCodeInvalid
	}
	return accepted, nil
}

// TOTPOtpauthURL builds the otpauth:// URI consumed by authenticator apps. The
// issuer and account keep the URL free of secrets other than the shared
// secret itself, which enrollment must display exactly once. The label
// percent-encodes the issuer separator so strict URI parsers stay happy.
func TOTPOtpauthURL(account string, encodedSecret string) string {
	label := strings.ReplaceAll(url.PathEscape("RelayShelf:"+account), ":", "%3A")
	query := url.Values{}
	query.Set("secret", encodedSecret)
	query.Set("issuer", "RelayShelf")
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(TOTPDigits))
	query.Set("period", fmt.Sprint(TOTPPeriodSeconds))
	return "otpauth://totp/" + label + "?" + query.Encode()
}
