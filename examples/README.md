# examples

Example requests and operator workflows for Hecate.

Contents:

- `chat/basic-cloud-request.json`: simple cloud-targeted chat request
- `chat/local-tenant-request.json`: local-provider request with tenant identity
- `curl/basic-cloud.sh`: minimal curl example
- `curl/local-explicit-provider.sh`: explicit local-provider routing example
- `curl/budget-status.sh`: inspect a scoped budget

Typical flow:

1. Start the gateway with `make dev`
2. Start the UI with `make ui-dev`
3. Run one of the curl examples or paste one of the JSON payloads into a client

Tips:

- The gateway accepts an OpenAI-compatible `user` field, which Hecate currently uses as tenant identity
- The gateway also accepts an optional `provider` field to explicitly target a provider from the UI or API
- For local models, install the model in your local runtime first, for example `ollama pull llama3.2:3b`
