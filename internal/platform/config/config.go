package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultStorageRoot = "/storage"
	defaultStagingRoot = "/staging"
)

type Config struct {
	DatabaseURL                  Secret
	StorageRoot, StagingRoot     string
	AppEncryptionKey, CSRFSecret Secret
	PublicOrigin                 *url.URL
	TrustedProxies               []netip.Prefix
	FileFinalizeConcurrency      int
	ThumbnailWorkers             int
	MaxActiveChunkWrites         int
	UploadStagingMaxBytes        int64
	StagingMinFreeBytes          int64
	StagingMinFreePercent        int
}

type StorageConfig struct{ StorageRoot string }

// LoadStorageConfig intentionally reads only the setting required by
// `relayshelf storage check`; it must remain independent from DB/auth secrets.
func LoadStorageConfig() (StorageConfig, error) {
	root, err := absolutePath("STORAGE_ROOT", defaultStorageRoot)
	return StorageConfig{StorageRoot: root}, err
}

func (c Config) String() string {
	return fmt.Sprintf("config{database_url:%s, app_encryption_key:%s, csrf_secret:%s}", c.DatabaseURL, c.AppEncryptionKey, c.CSRFSecret)
}

// Load reads the process environment once and validates all deployment values.
func Load() (Config, error) {
	databaseURL, err := LoadDatabaseURL()
	if err != nil {
		return Config{}, err
	}
	storageRoot, err := absolutePath("STORAGE_ROOT", defaultStorageRoot)
	if err != nil {
		return Config{}, err
	}
	stagingRoot, err := absolutePath("STAGING_ROOT", defaultStagingRoot)
	if err != nil {
		return Config{}, err
	}
	if storageRoot == stagingRoot {
		return Config{}, errors.New("STORAGE_ROOT and STAGING_ROOT must differ")
	}
	key, err := base64Secret("APP_ENCRYPTION_KEY", 32, true)
	if err != nil {
		return Config{}, err
	}
	csrf, err := base64Secret("CSRF_SECRET", 32, false)
	if err != nil {
		return Config{}, err
	}
	origin, err := publicOrigin()
	if err != nil {
		return Config{}, err
	}
	proxies, err := trustedProxies()
	if err != nil {
		return Config{}, err
	}
	finalize, err := positiveInt("FILE_FINALIZE_CONCURRENCY", 1)
	if err != nil {
		return Config{}, err
	}
	thumbs, err := positiveInt("THUMBNAIL_WORKERS", 1)
	if err != nil {
		return Config{}, err
	}
	writes, err := positiveInt("MAX_ACTIVE_CHUNK_WRITES", 8)
	if err != nil {
		return Config{}, err
	}
	stagingMax, err := positiveInt64("UPLOAD_STAGING_MAX_BYTES", 34359738368)
	if err != nil {
		return Config{}, err
	}
	minFree, err := nonNegativeInt64("STAGING_MIN_FREE_BYTES", 10737418240)
	if err != nil {
		return Config{}, err
	}
	minPercent, err := rangedInt("STAGING_MIN_FREE_PERCENT", 10, 0, 100)
	if err != nil {
		return Config{}, err
	}
	return Config{databaseURL, storageRoot, stagingRoot, key, csrf, origin, proxies, finalize, thumbs, writes, stagingMax, minFree, minPercent}, nil
}

// LoadDatabaseURL is deliberately narrow so the explicit migrate command does
// not depend on storage mounts or application secrets.
func LoadDatabaseURL() (Secret, error) {
	raw, err := requiredPostgresURL("DATABASE_URL")
	if err != nil {
		return Secret{}, err
	}
	return newSecret([]byte(raw)), nil
}

func requiredPostgresURL(name string) (string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", fmt.Errorf("%s must be a valid PostgreSQL URL", name)
	}
	return raw, nil
}
func absolutePath(name, fallback string) (string, error) {
	raw := os.Getenv(name)
	if raw == "" {
		raw = fallback
	}
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("%s must be an absolute path", name)
	}
	return filepath.Clean(raw), nil
}
func base64Secret(name string, min int, exact bool) (Secret, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return Secret{}, fmt.Errorf("%s is required", name)
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || (exact && len(b) != min) || (!exact && len(b) < min) {
		if exact {
			return Secret{}, fmt.Errorf("%s must be base64 for exactly %d bytes", name, min)
		}
		return Secret{}, fmt.Errorf("%s must be base64 for at least %d bytes", name, min)
	}
	allZero := true
	for _, value := range b {
		if value != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return Secret{}, fmt.Errorf("%s must not use an all-zero value", name)
	}
	return newSecret(b), nil
}
func publicOrigin() (*url.URL, error) {
	raw := strings.TrimSpace(os.Getenv("PUBLIC_ORIGIN"))
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("PUBLIC_ORIGIN must be an absolute http(s) URL without query, fragment, or path")
	}
	return u, nil
}
func trustedProxies() ([]netip.Prefix, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES contains an invalid CIDR")
		}
		out = append(out, prefix)
	}
	return out, nil
}
func positiveInt(name string, fallback int) (int, error) {
	n, err := rangedInt(name, fallback, 1, int(^uint(0)>>1))
	return n, err
}
func positiveInt64(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return n, nil
}
func nonNegativeInt64(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	return n, nil
}
func rangedInt(name string, fallback, min, max int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return n, nil
}
