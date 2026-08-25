package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:secret@localhost:5432/db?sslmode=disable")
	t.Setenv("STORAGE_ROOT", "/storage")
	t.Setenv("STAGING_ROOT", "/staging")
	t.Setenv("APP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	t.Setenv("CSRF_SECRET", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)))
	t.Setenv("PUBLIC_ORIGIN", "https://example.test")
	t.Setenv("TRUSTED_PROXIES", "")
}
func TestLoad(t *testing.T) {
	cases := []struct{ name, key, value string }{{"missing database", "DATABASE_URL", ""}, {"malformed database", "DATABASE_URL", "http://example.test"}, {"invalid key", "APP_ENCRYPTION_KEY", "abc"}, {"invalid csrf", "CSRF_SECRET", "abc"}, {"relative root", "STORAGE_ROOT", "storage"}, {"same roots", "STAGING_ROOT", "/storage"}, {"bad origin", "PUBLIC_ORIGIN", "https://example.test/path"}, {"bad cidr", "TRUSTED_PROXIES", "invalid"}, {"bad concurrency", "THUMBNAIL_WORKERS", "0"}, {"bad quota", "UPLOAD_STAGING_MAX_BYTES", "0"}, {"bad percent", "STAGING_MIN_FREE_PERCENT", "101"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("Load succeeded")
			}
		})
	}
	t.Run("happy path", func(t *testing.T) {
		validEnv(t)
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if c.FileFinalizeConcurrency != 1 || len(c.TrustedProxies) != 0 {
			t.Fatal("unexpected defaults")
		}
	})
	t.Run("redacts", func(t *testing.T) {
		validEnv(t)
		c, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{"postgres://user:secret@localhost:5432/db?sslmode=disable", string(c.AppEncryptionKey.Bytes()), string(c.CSRFSecret.Bytes())} {
			if strings.Contains(fmt.Sprintf("%v %#v %s", c.DatabaseURL, c.AppEncryptionKey, c), value) {
				t.Fatalf("secret leaked: %q", value)
			}
		}
	})
}

func TestLoadStorageConfigDoesNotRequireApplicationSecrets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("STORAGE_ROOT", root)
	for _, name := range []string{"DATABASE_URL", "APP_ENCRYPTION_KEY", "CSRF_SECRET", "PUBLIC_ORIGIN"} {
		t.Setenv(name, "")
	}
	cfg, err := LoadStorageConfig()
	if err != nil || cfg.StorageRoot != root {
		t.Fatalf("storage config=%+v %v", cfg, err)
	}
}
