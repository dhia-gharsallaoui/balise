package main

import (
	"bytes"
	"strings"
	"testing"
)

// A reindex that fails prints the command's usage, and cobra's usage shows each flag's default.
// The DSN's default came from $BALISE_DSN, so its password could end up in logs.
func TestUsageNeverShowsTheDSNFromTheEnvironment(t *testing.T) {
	const secret = "postgresql://balise:s3cr3t-pass@127.0.0.1:5434/balise"
	t.Setenv("BALISE_DSN", secret)

	cmd := root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"reindex", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out.String(), "s3cr3t-pass") {
		t.Fatalf("usage leaks the DSN from $BALISE_DSN:\n%s", out.String())
	}
}

func TestTheDSNStillComesFromTheEnvironment(t *testing.T) {
	const secret = "postgresql://balise:s3cr3t-pass@127.0.0.1:5434/balise"
	t.Setenv("BALISE_DSN", secret)
	if got := resolveDSN(""); got != secret {
		t.Fatalf("resolveDSN(\"\") = %q, want the $BALISE_DSN value", got)
	}
	if got := resolveDSN("postgresql://x"); got != "postgresql://x" {
		t.Fatalf("an explicit --dsn must win, got %q", got)
	}
}
