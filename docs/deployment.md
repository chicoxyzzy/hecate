# Deployment

The [Quick Start](../README.md#quick-start) covers `docker compose up` end-to-end. This page is the reference for everything past the first run: pinning images, optional services, recovering a lost admin token, and resetting state.

## Image pinning

`docker-compose.yml` references `ghcr.io/chicoxyzzy/hecate:latest`, a multi-arch (`linux/amd64`, `linux/arm64`) image published from this repo on every `v*` tag. A fresh host can `docker compose pull` and start without a build step.

To pin to a specific release, replace `:latest` with the published tag (no `v` prefix — goreleaser uses the bare semver as the docker tag). Example for the current alpha:

```yaml
# docker-compose.yml
image: ghcr.io/chicoxyzzy/hecate:0.1.0-alpha.1
```

Pinning is recommended for any deployment beyond local experimentation — `:latest` floats over alpha increments that may include schema or config changes.

When the working tree is a checkout of the source, `docker compose up` rebuilds locally from the bundled `Dockerfile` instead of pulling. Useful for testing changes; remove the `image:` line or run `docker compose build` first if you want the local build to be the canonical artifact.

## Binary install

The release workflow publishes static, single-file binaries for `linux+darwin × amd64+arm64` to GitHub Releases. Skip Docker if you'd rather run the gateway directly:

```bash
# pick the right tarball for your OS / arch
curl -LO https://github.com/chicoxyzzy/hecate/releases/download/v0.1.0-alpha.1/hecate_0.1.0-alpha.1_linux_amd64.tar.gz
tar -xzf hecate_0.1.0-alpha.1_linux_amd64.tar.gz
./hecate
```

The binary embeds the React operator UI, listens on `:8080` by default, generates an admin bearer token on first boot (saved under `GATEWAY_DATA_DIR`, default `.data/`), and prints it once to stderr. No additional runtime dependencies — the binary is statically linked and CGO-free.

To pin the data directory to a known location:

```bash
GATEWAY_DATA_DIR=/var/lib/hecate ./hecate
```

For systemd, launchd, or supervisor wrappers, the only requirements are: the working directory is writable for `GATEWAY_DATA_DIR`, port 8080 is available, and `.env` (if used) sits in the working directory or is sourced into the unit file. The binary path itself can live anywhere on `$PATH`.

Available tarballs for `v0.1.0-alpha.1`:

- `hecate_0.1.0-alpha.1_linux_amd64.tar.gz`
- `hecate_0.1.0-alpha.1_linux_arm64.tar.gz`
- `hecate_0.1.0-alpha.1_darwin_amd64.tar.gz`
- `hecate_0.1.0-alpha.1_darwin_arm64.tar.gz`

Each tarball includes the binary plus `LICENSE` and `README.md`. Verify integrity against `checksums.txt` published alongside the release.

## Optional services (compose profiles)

```bash
docker compose --profile postgres up    # adds Postgres on :5432 for durable state
docker compose --profile ollama up      # adds Ollama on :11434 for local models
docker compose --profile full up        # both of the above
```

Profiles are additive — `--profile postgres --profile ollama` works too. Profiles are off by default so a bare `docker compose up` stays "just the gateway" with no extra containers.

To use the Postgres profile across subsystems, point each backend at it via env vars in `.env`:

```bash
GATEWAY_CONTROL_PLANE_BACKEND=postgres
GATEWAY_TASKS_BACKEND=postgres
# ... etc, see Storage backends below
POSTGRES_DSN=postgres://hecate:hecate@postgres:5432/hecate?sslmode=disable
```

## Auth and generated state

Hecate can start with almost no secrets in the environment. If `GATEWAY_AUTH_TOKEN` is unset, the gateway generates an admin bearer token on first run, prints it once, and stores bootstrap metadata under `GATEWAY_DATA_DIR`.

| Variable | Default | Notes |
|---|---|---|
| `GATEWAY_AUTH_TOKEN` | generated | Admin bearer token. Prefer the generated first-run token for local and single-host setups. |
| `GATEWAY_DATA_DIR` | `.data` locally, `/data` in Docker | Holds bootstrap metadata and local state files. |
| `GATEWAY_CONTROL_PLANE_SECRET_KEY` | development fallback | Encrypts persisted provider credentials. Set a strong value before sharing a deployment. |

## Recovering a lost admin token

The first-run banner is the easiest path. If it's scrolled out of `docker compose logs`, the token also lives in the bootstrap file on the `hecate-data` volume. The gateway image is distroless (no shell), so use `docker compose cp` to copy the file out:

```bash
docker compose cp hecate:/data/hecate.bootstrap.json - | tar -xO | jq -r .admin_token
```

(`docker compose cp ... -` emits a tar archive, hence the `tar -xO`.)

The bootstrap file (and the SQLite database, see below) persist across container restarts as long as the `hecate-data` volume sticks around — only `docker compose down -v` (or `make reset-docker`) wipes them.

## Resetting state

To wipe the stack back to first-run — removes the `hecate-data` (admin token + SQLite db), `postgres-data`, and `ollama-models` volumes and regenerates the admin token on the next `docker compose up`:

```bash
make reset-docker
```

The next page load in the browser detects the rejected stale token and re-prompts for the regenerated one — no manual `localStorage` cleanup needed.

For local (non-Docker) development resets, see [`development.md`](development.md#reset-state).

## Storage backends

Hecate keeps the storage model intentionally boring: each subsystem chooses a backend independently, usually `memory`, `sqlite`, or `postgres`.

| Subsystem | Env var | memory | sqlite | postgres |
|---|---|---:|---:|---:|
| Control plane | `GATEWAY_CONTROL_PLANE_BACKEND` | local default | Docker default | yes |
| API key auth | `GATEWAY_AUTH_BACKEND` | local default | Docker default | yes |
| Provider credentials | `GATEWAY_PROVIDER_STORE_BACKEND` | local default | Docker default | yes |
| Pricebook | `GATEWAY_PRICEBOOK_BACKEND` | local default | Docker default | yes |
| Budget / balances | `GATEWAY_BUDGET_BACKEND` | local default | Docker default | yes |
| Usage ledger | `GATEWAY_USAGE_BACKEND` | local default | Docker default | yes |
| Audit events | `GATEWAY_AUDIT_BACKEND` | local default | Docker default | yes |
| Policy rules | `GATEWAY_POLICY_BACKEND` | local default | Docker default | yes |
| Exact cache | `GATEWAY_CACHE_BACKEND` | local default | Docker default | yes |
| Semantic cache | `GATEWAY_SEMANTIC_CACHE_BACKEND` | yes | no | yes |
| Trace snapshots | `GATEWAY_TRACE_STORE_BACKEND` | local default | Docker default | yes |
| Retention history | `GATEWAY_RETENTION_HISTORY_BACKEND` | local default | Docker default | yes |
| Chat sessions | `GATEWAY_CHAT_SESSIONS_BACKEND` | local default | Docker default | yes |
| Tasks | `GATEWAY_TASKS_BACKEND` | local default | Docker default | yes |
| Task queue | `GATEWAY_TASK_QUEUE_BACKEND` | local default | Docker default | yes |

Deployment-specific notes:

- The docker image **defaults to `sqlite`** for every durable subsystem, persisting state at `GATEWAY_SQLITE_PATH` (default `/data/hecate.db` on the `hecate-data` volume). This is why `docker compose up` keeps tenants, keys, pricebook, tasks, and chat sessions across restarts with no extra config.
- The semantic cache has no SQLite backend and stays on `memory` in the docker image. To get persistent semantic search, switch just that subsystem to Postgres with `GATEWAY_SEMANTIC_CACHE_BACKEND=postgres`.
- `POSTGRES_DSN` is required when any subsystem uses `postgres`.
- To make any subsystem ephemeral in docker, override its backend via `.env` or compose env: `GATEWAY_TASKS_BACKEND=memory`, etc.

## Rate limiting

Rate limiting is a per-key token bucket. It is disabled by default so first-run local testing does not surprise users.

| Variable | Default | Notes |
|---|---:|---|
| `GATEWAY_RATE_LIMIT_ENABLED` | `false` | Enables per-key request rate limits. |
| `GATEWAY_RATE_LIMIT_RPM` | `60` | Steady-state refill rate per API key. |
| `GATEWAY_RATE_LIMIT_BURST` | `0` | Optional burst capacity. `0` means "same as RPM". |

Over-limit requests return `429 Too Many Requests` with `code: "rate_limit_exceeded"` and standard `X-RateLimit-*` headers. Admin bearer traffic and anonymous traffic share a single `anonymous` bucket because they do not have tenant key IDs.
