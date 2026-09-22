# Contributing

Issues and pull requests are welcome. Before opening a PR:

- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] New behavior has a test that fails without the change
- [ ] Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)

For questions or ideas, open an issue rather than a PR.

## Development

```bash
make help     # list targets
make test     # go test -race -count=1 ./...
make lint     # go vet, plus golangci-lint when installed
make build    # static binary into ./bin
make run      # run locally, sourcing .env
make up       # docker compose up --build
```

Requires Go 1.27+. Tests use doubles throughout; the Cloudflare API is simulated with
`httptest`. **No test makes a live call.**

```
cmd/relay          entrypoint and wiring
internal/smtpserver  SMTP submission server, AUTH, session handling
internal/email       parsing, policy, message model
internal/cloudflare  REST and Worker transports, error classification
internal/config      environment parsing, validation, redaction
internal/logging     structured delivery records
internal/health      liveness endpoint and probe mode
worker/              optional Cloudflare Worker transport
scripts/             release tooling, not part of the build
```

## Releasing

Releases are cut by running the **Release** workflow, either from the Actions tab or with:

```bash
gh workflow run release.yml -f version=1.1.0
```

Pass the version **without** a leading `v` (`1.1.0`, not `v1.1.0`). The workflow requires
write access to the repository, so only maintainers can run it. It moves the CHANGELOG's
`[Unreleased]` entries into a new dated version section, tags the release, publishes the
GitHub release, and triggers the container image build for that tag.

The workflow refuses to release unless four things hold: the version is bare semver, the tag
does not already exist, `[Unreleased]` has content, and the CI run for the exact commit being
released concluded `success`. A green run on an older commit does not count.
