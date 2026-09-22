package config

import (
	"net"
	"strconv"
)

// SMTPAddr returns the SMTP listener address as "host:port".
func (c *Config) SMTPAddr() string {
	return net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort))
}

// HealthAddr returns the health-check listener address as "host:port".
func (c *Config) HealthAddr() string {
	return net.JoinHostPort(c.HealthHost, strconv.Itoa(c.HealthPort))
}
