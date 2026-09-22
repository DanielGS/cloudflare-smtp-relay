package config

import "testing"

func TestSMTPAddr_JoinsHostAndPort(t *testing.T) {
	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"SMTP_HOST": "127.0.0.1",
		"SMTP_PORT": "2526",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.SMTPAddr(), "127.0.0.1:2526"; got != want {
		t.Errorf("SMTPAddr() = %q, want %q", got, want)
	}
}

func TestHealthAddr_JoinsHostAndPort(t *testing.T) {
	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"HEALTH_HOST": "10.0.0.5",
		"HEALTH_PORT": "9090",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.HealthAddr(), "10.0.0.5:9090"; got != want {
		t.Errorf("HealthAddr() = %q, want %q", got, want)
	}
}

func TestSMTPAddr_JoinsIPv6HostAndPort(t *testing.T) {
	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"SMTP_HOST": "::1",
		"SMTP_PORT": "2525",
	})))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := cfg.SMTPAddr(), "[::1]:2525"; got != want {
		t.Errorf("SMTPAddr() = %q, want %q", got, want)
	}
}
