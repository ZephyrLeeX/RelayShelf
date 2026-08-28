package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ZephyrLeeX/RelayShelf/internal/platform/config"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

// securityCheck is the T128 public-exposure operational gate. It verifies
// everything the process can verify by itself and explicitly names the checks
// that only a human can perform. It never prints secret values.
func securityCheck() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Security check: FAIL (invalid deployment configuration: %v)", err)
		os.Exit(1)
	}
	failures := 0
	manual := 0
	pass := func(format string, args ...any) {
		fmt.Printf("PASS  "+format+"\n", args...)
	}
	fail := func(format string, args ...any) {
		failures++
		fmt.Printf("FAIL  "+format+"\n", args...)
	}
	manualRequired := func(format string, args ...any) {
		manual++
		fmt.Printf("MANUAL  "+format+"\n", args...)
	}

	// Automated configuration checks.
	if cfg.PublicOrigin.Scheme == "https" {
		pass("PUBLIC_ORIGIN uses HTTPS; session cookie will be Secure (__Host- prefix)")
	} else {
		fail("PUBLIC_ORIGIN is %s; public exposure requires HTTPS so the session cookie gets the Secure attribute", cfg.PublicOrigin.Scheme)
	}
	if len(cfg.TrustedProxies) > 0 {
		pass("TRUSTED_PROXIES configured with %d prefix(es); forwarded headers are only honored from them", len(cfg.TrustedProxies))
	} else {
		manualRequired("TRUSTED_PROXIES is empty; confirm the reverse proxy path before trusting forwarded headers")
	}
	pass("APP_ENCRYPTION_KEY and CSRF_SECRET loaded and format-validated (values are never printed)")
	if cfg.StorageRoot != cfg.StagingRoot {
		pass("storage root and staging root are separate directories")
	}

	// Database and schema.
	ctx := context.Background()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		fail("database unreachable: %v", err)
		finishSecurity(failures, manual)
		return
	}
	defer func() { db.Close() }()
	if err = database.CheckCompatible(ctx, db); err != nil {
		fail("schema compatibility: %v", err)
	} else {
		current, _ := database.CurrentVersion(ctx, db)
		latest, _ := database.LatestVersion()
		pass("migrations compatible (schema %d of %d)", current, latest)
	}
	securityAdminTotp(ctx, db, pass, fail)

	// Storage reachability.
	adapter, err := storage.NewFilesystemStorageAdapter(cfg.StorageRoot)
	if err != nil {
		fail("storage root unavailable: %v", err)
	} else if _, probeErr := adapter.Space(ctx); probeErr != nil {
		fail("storage probe failed: %v", probeErr)
	} else {
		pass("storage root reachable")
	}

	// Operator confirmations that cannot be automated.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("RELAYSHELF_KEY_BACKUP_CONFIRMED")), "yes") {
		pass("encryption key backup confirmed by operator (RELAYSHELF_KEY_BACKUP_CONFIRMED)")
	} else {
		manualRequired("APP_ENCRYPTION_KEY has no confirmed off-machine backup; set RELAYSHELF_KEY_BACKUP_CONFIRMED=yes only after storing a copy")
	}
	manualRequired("TLS certificate health at the OpenWrt nginx terminator (must be verified at the proxy)")
	manualRequired("external nginx forwarding configuration (Host/X-Forwarded-*, cache and buffering rules)")
	finishSecurity(failures, manual)
}

func securityAdminTotp(ctx context.Context, db *pgxpool.Pool, pass func(string, ...any), fail func(string, ...any)) {
	var admins, without int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM users WHERE status='ACTIVE' AND is_admin`).Scan(&admins); err != nil {
		fail("active admin count: %v", err)
		return
	}
	if err := db.QueryRow(ctx, `SELECT count(*) FROM users u WHERE u.status='ACTIVE' AND u.is_admin AND NOT EXISTS(SELECT 1 FROM user_totp t WHERE t.user_id=u.id AND t.enabled_at IS NOT NULL)`).Scan(&without); err != nil {
		fail("admin TOTP query: %v", err)
		return
	}
	switch {
	case admins == 0:
		fail("no ACTIVE administrators exist")
	case without > 0:
		fail("%d of %d active administrator(s) have not confirmed TOTP", without, admins)
	default:
		pass("all %d active administrator(s) have confirmed TOTP", admins)
	}
}

func finishSecurity(failures, manual int) {
	fmt.Println("---")
	if failures > 0 {
		fmt.Printf("PUBLIC EXPOSURE SAFETY GATE: NOT READY (%d failing check(s), %d manual)\n", failures, manual)
		os.Exit(1)
	}
	if manual > 0 {
		fmt.Printf("PUBLIC EXPOSURE SAFETY GATE: NOT READY (0 failing, %d manual qualification(s) pending)\n", manual)
		os.Exit(1)
	}
	fmt.Println("PUBLIC EXPOSURE SAFETY GATE: READY")
}
