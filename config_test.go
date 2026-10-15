package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setEnv isolates a test from the real environment. kingpin treats an empty
// environment variable as unset.
func setEnv(t *testing.T, url, key, keyFile string) {
	t.Helper()
	t.Setenv("MISSKEY_URL", url)
	t.Setenv("MISSKEY_API_KEY", key)
	t.Setenv("MISSKEY_API_KEY_FILE", keyFile)
}

func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseConfigDefaults(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://example.com"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://example.com" {
		t.Errorf("MisskeyURL = %q", cfg.MisskeyURL)
	}
	if cfg.APIKey != "secret-token" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
	if cfg.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
	}
	if got := *cfg.Web.WebListenAddresses; len(got) != 1 || got[0] != ":9100" {
		t.Errorf("WebListenAddresses = %v, want [:9100]", got)
	}
}

func TestParseConfigURLFromEnv(t *testing.T) {
	setEnv(t, "https://env.example.com", "secret-token", "")

	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://env.example.com" {
		t.Errorf("MisskeyURL = %q", cfg.MisskeyURL)
	}
}

func TestParseConfigFlagOverridesEnv(t *testing.T) {
	setEnv(t, "https://env.example.com", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://flag.example.com"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.MisskeyURL != "https://flag.example.com" {
		t.Errorf("MisskeyURL = %q, want the flag value", cfg.MisskeyURL)
	}
}

func TestParseConfigURLNormalization(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"http://localhost:3000/", "http://localhost:3000"},
		// Misskey behind a reverse proxy under a sub path.
		{"https://example.com/misskey/", "https://example.com/misskey"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			cfg, err := parseConfig([]string{"--misskey.url=" + tt.in})
			if err != nil {
				t.Fatalf("parseConfig: %v", err)
			}
			if cfg.MisskeyURL != tt.want {
				t.Errorf("MisskeyURL = %q, want %q", cfg.MisskeyURL, tt.want)
			}
		})
	}
}

func TestParseConfigInvalidURL(t *testing.T) {
	tests := []string{
		"example.com",
		"ftp://example.com",
		"https://",
		"https://user:pass@example.com",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			_, err := parseConfig([]string{"--misskey.url=" + in})
			if err == nil {
				t.Fatalf("parseConfig(%q) succeeded, want error", in)
			}
			if !strings.Contains(err.Error(), "misskey.url") {
				t.Errorf("error %q does not mention misskey.url", err)
			}
			if strings.Contains(err.Error(), "pass") {
				t.Errorf("error %q leaks URL credentials", err)
			}
		})
	}
}

func TestParseConfigMissingURL(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	_, err := parseConfig(nil)
	if err == nil || !strings.Contains(err.Error(), "misskey.url") {
		t.Fatalf("err = %v, want an error mentioning misskey.url", err)
	}
}

func TestParseConfigTimeout(t *testing.T) {
	setEnv(t, "", "secret-token", "")

	cfg, err := parseConfig([]string{"--misskey.url=https://example.com", "--misskey.timeout=2s"})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want 2s", cfg.Timeout)
	}

	for _, bad := range []string{"0s", "-1s", "abc"} {
		t.Run(bad, func(t *testing.T) {
			setEnv(t, "", "secret-token", "")
			_, err := parseConfig([]string{"--misskey.url=https://example.com", "--misskey.timeout=" + bad})
			if err == nil {
				t.Fatalf("timeout %q accepted, want error", bad)
			}
		})
	}
}

func TestParseConfigAPIKeySources(t *testing.T) {
	args := []string{"--misskey.url=https://example.com"}

	t.Run("env only", func(t *testing.T) {
		setEnv(t, "", "env-token", "")
		cfg, err := parseConfig(args)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "env-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})

	t.Run("file only", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, "file-token")
		cfg, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})

	// Docker secrets and Kubernetes secrets are often written with a trailing
	// newline.
	t.Run("file with surrounding whitespace", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, "  file-token\r\n")
		cfg, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q, want the trimmed token", cfg.APIKey)
		}
	})

	t.Run("file path from env", func(t *testing.T) {
		path := writeKeyFile(t, "file-token")
		setEnv(t, "", "", path)
		cfg, err := parseConfig(args)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.APIKey != "file-token" {
			t.Errorf("APIKey = %q", cfg.APIKey)
		}
	})
}

func TestParseConfigAPIKeyErrors(t *testing.T) {
	args := []string{"--misskey.url=https://example.com"}

	t.Run("both set", func(t *testing.T) {
		path := writeKeyFile(t, "file-token")
		setEnv(t, "", "env-token-xyz", "")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err == nil || !strings.Contains(err.Error(), "only one") {
			t.Fatalf("err = %v, want a 'specify only one' error", err)
		}
		for _, secret := range []string{"env-token-xyz", "file-token"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q leaks the API key", err)
			}
		}
	})

	t.Run("neither set", func(t *testing.T) {
		setEnv(t, "", "", "")
		_, err := parseConfig(args)
		if err == nil || !strings.Contains(err.Error(), "MISSKEY_API_KEY") {
			t.Fatalf("err = %v, want an error mentioning MISSKEY_API_KEY", err)
		}
	})

	t.Run("whitespace-only env", func(t *testing.T) {
		setEnv(t, "", "  \n", "")
		if _, err := parseConfig(args); err == nil {
			t.Fatal("whitespace-only MISSKEY_API_KEY accepted, want error")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		setEnv(t, "", "", "")
		missing := filepath.Join(t.TempDir(), "nope")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+missing))
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Fatalf("err = %v, want an error naming the file", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		setEnv(t, "", "", "")
		path := writeKeyFile(t, " \n")
		_, err := parseConfig(append(args, "--misskey.api-key-file="+path))
		if err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("err = %v, want an 'empty' error", err)
		}
	})
}

func TestParseConfigReportsAllProblems(t *testing.T) {
	setEnv(t, "", "", "")

	_, err := parseConfig([]string{"--misskey.url=ftp://example.com", "--misskey.timeout=0s"})
	if err == nil {
		t.Fatal("parseConfig succeeded, want error")
	}
	for _, want := range []string{"misskey.url", "misskey.timeout", "MISSKEY_API_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
