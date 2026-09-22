package config

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// aggregateError joins every validation problem into a single error, so an
// operator sees every mistake in one pass instead of fixing them one at a
// time.
func aggregateError(problems []string) error {
	errs := make([]error, 0, len(problems))
	for _, p := range problems {
		errs = append(errs, errors.New(p))
	}
	return errors.Join(errs...)
}

func intOrDefault(getenv getenvFunc, key string, def int, addProblem func(string, ...any)) int {
	v, ok := getenv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		addProblem("%s: expected an integer, got %q", key, v)
		return def
	}
	return n
}

func int64OrDefault(getenv getenvFunc, key string, def int64, addProblem func(string, ...any)) int64 {
	v, ok := getenv(key)
	if !ok || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		addProblem("%s: expected an integer, got %q", key, v)
		return def
	}
	return n
}

func durationOrDefault(getenv getenvFunc, key string, def time.Duration, addProblem func(string, ...any)) time.Duration {
	v, ok := getenv(key)
	if !ok || v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		addProblem("%s: expected a duration (e.g. \"30s\"), got %q", key, v)
		return def
	}
	return d
}

// validatePortRange checks that a port number is in the valid TCP port
// range (1-65535), recording a problem naming key when it is not.
func validatePortRange(port int, key string, addProblem func(string, ...any)) {
	if port < 1 || port > 65535 {
		addProblem("%s: must be between 1 and 65535, got %d", key, port)
	}
}

// validateAbsoluteHTTPURL checks that raw parses as an absolute http or
// https URL, recording a problem naming key when it does not.
func validateAbsoluteHTTPURL(raw, key string, addProblem func(string, ...any)) {
	u, err := url.Parse(raw)
	if err != nil {
		addProblem("%s: must be a valid URL, got %q: %v", key, raw, err)
		return
	}
	if !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		addProblem("%s: must be an absolute http or https URL, got %q", key, raw)
	}
}

// normalizeDomains splits a comma-separated domain list, trimming
// whitespace, lowercasing, dropping empty entries and deduplicating.
func normalizeDomains(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	var out []string
	for _, p := range parts {
		d := strings.ToLower(strings.TrimSpace(p))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}
