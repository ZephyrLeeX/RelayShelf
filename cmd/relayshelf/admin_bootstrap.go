package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ZephyrLeeX/RelayShelf/internal/audit"
	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/config"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/users"
	"golang.org/x/term"
)

var errBootstrapSchemaIncompatible = errors.New("database schema incompatible")

type passwordReader interface {
	ReadPassword(prompt string) (string, error)
}

type terminalPasswordReader struct {
	input  *os.File
	output io.Writer
}

func newTerminalPasswordReader(input *os.File, output io.Writer) passwordReader {
	return &terminalPasswordReader{input: input, output: output}
}

func (r *terminalPasswordReader) ReadPassword(prompt string) (string, error) {
	fd := int(r.input.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("password input requires a terminal")
	}
	if _, err := fmt.Fprint(r.output, prompt); err != nil {
		return "", err
	}
	secret, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(r.output)
	if err != nil {
		return "", err
	}
	return string(secret), nil
}

type bootstrapAdminFunc func(context.Context, string, string, string) (users.User, error)

func adminBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, reader passwordReader) int {
	return runAdminBootstrap(ctx, args, stdout, stderr, reader, bootstrapInitialAdmin)
}

func runAdminBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, reader passwordReader, bootstrap bootstrapAdminFunc) int {
	flags := flag.NewFlagSet("admin bootstrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // Never echo rejected argv; it may contain a password.
	username := flags.String("username", "", "administrator username")
	displayName := flags.String("display-name", "", "administrator display name")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *username == "" || *displayName == "" {
		writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (invalid arguments; require --username and --display-name; passwords are never accepted in argv)")
		return 2
	}

	password, err := reader.ReadPassword("Password: ")
	if err != nil {
		writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (secure terminal password input unavailable)")
		return 1
	}
	confirmation, err := reader.ReadPassword("Confirm password: ")
	if err != nil {
		writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (secure terminal password input unavailable)")
		return 1
	}
	if password != confirmation {
		writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (password confirmation mismatch)")
		return 1
	}
	if err = users.ValidatePassword(password); err != nil {
		writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (password policy)")
		return 1
	}

	user, err := bootstrap(ctx, *username, *displayName, password)
	if err != nil {
		switch {
		case errors.Is(err, users.ErrBootstrapUnavailable):
			writeBootstrapLine(stderr, "Initial administrator bootstrap: REFUSED (users already exist)")
		case errors.Is(err, users.ErrInvalidUsername):
			writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (invalid username or display name)")
		case errors.Is(err, users.ErrInvalidPassword):
			writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (password policy)")
		case errors.Is(err, errBootstrapSchemaIncompatible):
			writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (database schema incompatible)")
		default:
			writeBootstrapLine(stderr, "Initial administrator bootstrap: FAIL (database operation failed)")
		}
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "Initial administrator bootstrap: PASS\nUsername: %s\nAdministrator: yes\nStatus: ACTIVE\n", user.Username)
	return 0
}

func writeBootstrapLine(output io.Writer, message string) {
	_, _ = io.WriteString(output, message+"\n")
}

func bootstrapInitialAdmin(ctx context.Context, username, displayName, password string) (users.User, error) {
	databaseURL, err := config.LoadDatabaseURL()
	if err != nil {
		return users.User{}, err
	}
	db, err := database.Open(ctx, config.Config{DatabaseURL: databaseURL})
	if err != nil {
		return users.User{}, err
	}
	defer db.Close()
	if err = database.CheckCompatible(ctx, db); err != nil {
		return users.User{}, fmt.Errorf("%w: %v", errBootstrapSchemaIncompatible, err)
	}
	now := clock.Real{}
	recorder := audit.NewRecorder(id.UUIDv7{}, now)
	service := users.NewAdminService(db, auth.NewPasswordHasher(auth.DefaultArgon2Params), id.UUIDv7{}, now, recorder)
	return service.BootstrapInitialAdmin(ctx, username, displayName, password)
}
