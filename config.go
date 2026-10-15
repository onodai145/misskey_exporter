package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/common/promslog"
	promslogflag "github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

const (
	programName       = "misskey_exporter"
	defaultListenAddr = ":9100"
)

// config is the validated runtime configuration.
type config struct {
	MisskeyURL string        // normalized base URL, without a trailing slash
	APIKey     string        // access token; never log it
	Timeout    time.Duration // timeout of one scrape's API requests
	Web        *web.FlagConfig
	Log        *promslog.Config
}

// parseConfig parses command line arguments (without the program name) and the
// environment, then validates the result. It reports every problem it finds.
func parseConfig(args []string) (*config, error) {
	app := kingpin.New(programName, "Prometheus exporter for Misskey.")
	app.HelpFlag.Short('h')
	app.Version(version.Print(programName))

	rawURL := app.Flag("misskey.url", "Base URL of the Misskey instance (http or https).").
		Envar("MISSKEY_URL").Required().String()
	keyFile := app.Flag("misskey.api-key-file",
		"Path to a file that contains the Misskey access token. "+
			"Alternative to the MISSKEY_API_KEY environment variable.").
		Envar("MISSKEY_API_KEY_FILE").String()
	timeout := app.Flag("misskey.timeout",
		"Timeout of the Misskey API requests of one scrape. Keep it shorter than the Prometheus scrape interval.").
		Default("5s").Duration()

	cfg := &config{Log: &promslog.Config{}}
	cfg.Web = webflag.AddFlags(app, defaultListenAddr)
	promslogflag.AddFlags(app, cfg.Log)

	if _, err := app.Parse(args); err != nil {
		return nil, err
	}

	var errs []error

	baseURL, err := normalizeBaseURL(*rawURL)
	if err != nil {
		errs = append(errs, err)
	}
	if *timeout <= 0 {
		errs = append(errs, errors.New("--misskey.timeout must be greater than 0"))
	}
	apiKey, err := resolveAPIKey(os.Getenv("MISSKEY_API_KEY"), *keyFile)
	if err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	cfg.MisskeyURL = baseURL
	cfg.APIKey = apiKey
	cfg.Timeout = *timeout
	return cfg, nil
}

// normalizeBaseURL checks that raw is an http(s) URL and strips a trailing
// slash so that "<base>/api/..." never contains a double slash.
func normalizeBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid --misskey.url: cannot be parsed as a URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("invalid --misskey.url: scheme must be http or https")
	}
	if u.Host == "" {
		return "", errors.New("invalid --misskey.url: host is missing")
	}
	if u.User != nil {
		return "", errors.New("invalid --misskey.url: must not contain credentials")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// resolveAPIKey returns the access token from either the environment value or
// the key file. Specifying both is an error so that there is no precedence
// rule to remember. Error messages never contain the token.
func resolveAPIKey(envValue, keyFile string) (string, error) {
	envValue = strings.TrimSpace(envValue)
	switch {
	case envValue != "" && keyFile != "":
		return "", errors.New("both MISSKEY_API_KEY and --misskey.api-key-file are set; specify only one")
	case envValue != "":
		return envValue, nil
	case keyFile != "":
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return "", fmt.Errorf("read API key file: %w", err)
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", fmt.Errorf("API key file %q is empty", keyFile)
		}
		return key, nil
	default:
		return "", errors.New("API key is not set; set MISSKEY_API_KEY or --misskey.api-key-file")
	}
}
