package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/users"
)

type scriptedPasswordReader struct {
	values []string
	errors []error
	calls  int
}

func TestTerminalPasswordReaderRejectsNonTTYWithoutPromptOrEcho(t *testing.T) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = inputWriter.Close()
	})
	var output bytes.Buffer
	reader := newTerminalPasswordReader(input, &output)
	if _, err = reader.ReadPassword("Password: "); err == nil {
		t.Fatal("non-TTY password input must fail closed")
	}
	if output.Len() != 0 {
		t.Fatalf("non-TTY path wrote a prompt or input: %q", output.String())
	}
}

func (r *scriptedPasswordReader) ReadPassword(string) (string, error) {
	index := r.calls
	r.calls++
	if index < len(r.errors) && r.errors[index] != nil {
		return "", r.errors[index]
	}
	if index >= len(r.values) {
		return "", io.EOF
	}
	return r.values[index], nil
}

func TestAdminBootstrapReadsConfirmedPasswordWithoutOutputtingIt(t *testing.T) {
	const password = "bootstrap-secret-password"
	reader := &scriptedPasswordReader{values: []string{password, password}}
	var stdout, stderr bytes.Buffer
	called := false
	code := runAdminBootstrap(context.Background(), []string{"--username", " Admin ", "--display-name", "Initial Admin"}, &stdout, &stderr, reader,
		func(_ context.Context, username, displayName, gotPassword string) (users.User, error) {
			called = true
			if username != " Admin " || displayName != "Initial Admin" || gotPassword != password {
				t.Fatalf("unexpected command: username=%q display=%q password matched=%v", username, displayName, gotPassword == password)
			}
			return users.User{Username: "admin", IsAdmin: true, Status: users.StatusActive}, nil
		})
	if code != 0 || !called || reader.calls != 2 {
		t.Fatalf("code=%d called=%v reads=%d stderr=%q", code, called, reader.calls, stderr.String())
	}
	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, password) || strings.Contains(strings.ToLower(combined), "argon2") {
		t.Fatalf("secret material reached output: %q", combined)
	}
	if !strings.Contains(stdout.String(), "PASS") || !strings.Contains(stdout.String(), "Username: admin") {
		t.Fatalf("unexpected success output: %q", stdout.String())
	}
}

func TestAdminBootstrapPasswordInputFailuresFailClosed(t *testing.T) {
	const password = "bootstrap-secret-password"
	tests := []struct {
		name   string
		reader *scriptedPasswordReader
		want   string
	}{
		{name: "confirmation mismatch", reader: &scriptedPasswordReader{values: []string{password, "different-password"}}, want: "confirmation mismatch"},
		{name: "EOF", reader: &scriptedPasswordReader{errors: []error{io.EOF}}, want: "secure terminal"},
		{name: "confirmation read failure", reader: &scriptedPasswordReader{values: []string{password}, errors: []error{nil, errors.New("terminal failed")}}, want: "secure terminal"},
		{name: "validation failure", reader: &scriptedPasswordReader{values: []string{"short", "short"}}, want: "password policy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			called := false
			code := runAdminBootstrap(context.Background(), []string{"--username", "admin", "--display-name", "Admin"}, &stdout, &stderr, test.reader,
				func(context.Context, string, string, string) (users.User, error) {
					called = true
					return users.User{}, nil
				})
			if code == 0 || called || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("code=%d called=%v stdout=%q stderr=%q", code, called, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), password) {
				t.Fatal("password appeared in output")
			}
		})
	}
}

func TestAdminBootstrapRejectsPasswordArgvWithoutEchoingIt(t *testing.T) {
	const password = "argv-password-must-not-leak"
	var stdout, stderr bytes.Buffer
	reader := &scriptedPasswordReader{}
	code := runAdminBootstrap(context.Background(), []string{"--username", "admin", "--display-name", "Admin", "--password=" + password}, &stdout, &stderr, reader, nil)
	if code == 0 || reader.calls != 0 {
		t.Fatalf("code=%d reads=%d", code, reader.calls)
	}
	if strings.Contains(stdout.String()+stderr.String(), password) {
		t.Fatalf("argv secret echoed: %q", stdout.String()+stderr.String())
	}
}

func TestAdminBootstrapMapsDomainAndSchemaFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "existing users", err: users.ErrBootstrapUnavailable, want: "REFUSED (users already exist)"},
		{name: "invalid username", err: users.ErrInvalidUsername, want: "invalid username"},
		{name: "schema", err: errBootstrapSchemaIncompatible, want: "database schema incompatible"},
		{name: "internal", err: errors.New("SQL contains argv-password-must-not-leak"), want: "database operation failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &scriptedPasswordReader{values: []string{"valid-password", "valid-password"}}
			var stdout, stderr bytes.Buffer
			code := runAdminBootstrap(context.Background(), []string{"--username", "admin", "--display-name", "Admin"}, &stdout, &stderr, reader,
				func(context.Context, string, string, string) (users.User, error) { return users.User{}, test.err })
			if code == 0 || !strings.Contains(stderr.String(), test.want) || strings.Contains(stderr.String(), "argv-password") {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}
