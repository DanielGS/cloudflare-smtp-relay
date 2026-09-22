# Security Policy

Do not report security issues in a public issue. Email the maintainer instead.

The relay is designed to hold secrets and refuse to leak them: tokens and passwords are
redacted from configuration dumps, never written to logs, and the Worker compares its shared
secret in constant time. Run it on a private network; it has no TLS requirement because it is
not meant to be exposed to the internet.
