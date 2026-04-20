# deployments

Deployment-oriented assets and configuration templates for Hecate.

This directory currently focuses on pragmatic local and single-host setups rather than full production orchestration.

Contents:

- `env/cloud-only.env.example`: minimal cloud-provider gateway setup
- `env/local-first-ollama.env.example`: local-first setup with Ollama fallback to cloud
- `env/shared-budget-redis.env.example`: Redis-backed shared budget and cache example

Recommended usage:

1. Copy one of the env templates into the repo root as `.env`
2. Edit secrets and provider-specific values
3. Start the gateway with `make dev`
4. Start the UI with `make ui-dev`

Notes:

- These templates are intentionally shell-compatible because `make dev` sources `.env` directly
- Quote values that contain spaces
- For local providers, make sure the configured model is actually installed before routing traffic to it

Future work for this directory:

- container image and `Dockerfile`
- `docker-compose` setup for gateway + Redis + Ollama
- Kubernetes manifests or Helm chart
- reverse proxy and TLS examples
