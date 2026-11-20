package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, content string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	run := func(path string, env map[string]string) error {
		return LoadDotEnv(path, func(k string) string { return env[k] }, func(k, v string) error {
			env[k] = v
			return nil
		})
	}

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		if err := run(filepath.Join(t.TempDir(), "nope"), map[string]string{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("comments blanks and quotes", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{}
		p := write(t, "# comment\n\nA=1\n B = two \nC=\"three four\"\nD='five'\n")
		if err := run(p, env); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"A": "1", "B": "two", "C": "three four", "D": "five"}
		for k, v := range want {
			if env[k] != v {
				t.Errorf("%s = %q, want %q", k, env[k], v)
			}
		}
		if len(env) != len(want) {
			t.Errorf("unexpected env: %v", env)
		}
	})

	t.Run("does not override", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{"A": "existing"}
		if err := run(write(t, "A=new\nB=2\n"), env); err != nil {
			t.Fatal(err)
		}
		if env["A"] != "existing" || env["B"] != "2" {
			t.Fatalf("unexpected env: %v", env)
		}
	})

	t.Run("line without equals", func(t *testing.T) {
		t.Parallel()
		err := run(write(t, "A=1\nsuper-secret-value\n"), map[string]string{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ":2:") {
			t.Errorf("error %q lacks line number", err)
		}
		if strings.Contains(err.Error(), "super-secret-value") {
			t.Errorf("error %q leaks line content", err)
		}
	})
}
