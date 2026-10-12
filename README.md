# oc3-mcp

A Go Model Context Protocol server providing AI agents with typed, read-only
tools for the OpenSVC collector API.

## Status

- **HTTPS `/mcp`**, stateless Streamable HTTP, on a loopback address only.
- The MCP reads the collector with one account configured by the operator.
  It does not authenticate its clients yet: every caller sees what this
  account sees. This mode is temporary, until the collector accepts a token
  the MCP can forward for each caller.

See the [configuration](docs/configuration.md) and the
[tool documentation](docs/tools/README.md).

## Requirements

- Go 1.25.5 or later to build from source.
- An OpenSVC collector reachable over HTTPS, and an account on it.
- A TLS certificate and private key for the MCP listener.

## Build

```bash
go build -o bin/oc3-mcp ./cmd/oc3-mcp
```

## Run

```bash
OPENSVC_MCP_TLS_CERT_FILE=/etc/opensvc-oc3-mcp/server.crt \
OPENSVC_MCP_TLS_KEY_FILE=/etc/opensvc-oc3-mcp/server.key \
OPENSVC_MCP_COLLECTOR_URL=https://collector.example.com/api \
OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE=/etc/opensvc-oc3-mcp/collector.netrc \
  ./bin/oc3-mcp
```

Check liveness, which does not contact the collector:

```bash
curl --cacert /etc/opensvc-oc3-mcp/ca.pem https://127.0.0.1:8444/health
```
