# Release Process

AmanMCP release execution is gated by two local dogfooding checks:

- `make install-local-and-verify` rebuilds the current checkout, installs
  `amanmcp` to `~/.local/bin/amanmcp`, and verifies `amanmcp version --short`
  matches the repo `VERSION` file.
- `make release-rehearse` exercises release surfaces against explicit
  rehearsal remotes and writes a durable report under
  `.aman-pm/validation/release-rehearsal/`.

## Installed Binary Parity

Run this before PRs or release work that touches the CLI, version package,
MCP tool surface, or internal packages that affect shipped behavior:

```bash
make install-local-and-verify
```

The verifier prints the repo version, installed binary path, installed version,
mode, final status, and remediation command. `make ci-check` runs the same
check as an advisory during Sprint 16. `make ci-check-strict` runs full CI and
then blocks on the installed-binary parity gate.

## Release Rehearsal Setup

Rehearsal remotes must be personal forks or disposable test repositories. The
runner refuses canonical `Aman-CERP/amanmcp-raw` and `Aman-CERP/amanmcp`<!-- SYNC-OK: documenting canonical-remote refusal pattern -->
remotes and has no fallback to `origin`.

One-time setup example:

```bash
mkdir -p "$HOME/.local/code"
git clone git@github.com:YOUR_USER/amanmcp-rehearse.git "$HOME/.local/code/amanmcp-rehearse"

export AMANMCP_REHEARSE_RAW_REMOTE=git@github.com:YOUR_USER/amanmcp-raw-rehearse.git  # SYNC-OK: example URL pattern for personal rehearsal fork
export AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE=git@github.com:YOUR_USER/amanmcp-rehearse.git
export AMANMCP_REHEARSE_PUBLIC_REPO="$HOME/.local/code/amanmcp-rehearse"

./scripts/release-rehearse.sh --check-config
make release-rehearse
```

The public rehearsal clone origin must match
`AMANMCP_REHEARSE_PUBLIC_MIRROR_REMOTE`, because `scripts/sync-to-public.sh`
pushes the synced mirror through that clone's `origin`.

## Rehearsal Phases

`make release-rehearse` uses a disposable tag named
`v0.0.0-rehearse-YYYYMMDDHHMMSS` and records each phase:

1. Validate explicit non-canonical rehearsal remotes.
2. Create and push the rehearsal tag to the raw rehearsal remote.
3. Run `goreleaser release --snapshot --clean`.
4. Verify the produced artifact matrix matches the documented darwin/arm64
   snapshot matrix.
5. Sync the raw checkout to the rehearsal public mirror clone.
6. Verify the public mirror push landed.
7. Run `go test -race ./...` in the public mirror clone.
8. Delete the rehearsal tag locally and remotely.

Reports include commit SHA, tag, remotes, timestamps, phase commands, phase
exit codes, teardown status, and the failing phase log when red.

## Real Release Guard

`scripts/release.sh <version>` refuses a real release when the current commit
does not have a green rehearsal report from the last 24 hours. The override is
intentionally loud:

```bash
./scripts/release.sh --skip-rehearse-check vX.Y.Z
```

Use the override only with explicit owner sign-off. It bypasses the guard; it
does not create release evidence.
