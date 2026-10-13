# oc3-mcp

A Go Model Context Protocol server providing AI agents with typed, read-only
tools for the OpenSVC collector API.

## Status

- **Unix socket `/mcp`**, stateless Streamable HTTP, for OpenSVC components
  such as the AI agent.
- Every request carries the OpenID Connect access token of its user. The MCP
  forwards it unchanged to the collector, which verifies it: every caller sees
  what its own collector account sees. The MCP holds no account and never
  receives a password.

See the [configuration](docs/configuration.md) and the
[tool documentation](docs/tools/README.md).

## Requirements

- Go 1.25.5 or later to build from source.
- An OpenSVC collector reachable over HTTPS, accepting the OpenID Connect
  access tokens of its users and serving `GET /api/auth/whoami`.

## Build

```bash
go build -o bin/oc3-mcp ./cmd/oc3-mcp
```

## Run

```bash
OPENSVC_MCP_DELEGATED_SOCKET=/run/opensvc-oc3-mcp/delegated.sock \
OPENSVC_MCP_COLLECTOR_URL=https://collector.example.com/api \
  ./bin/oc3-mcp
```

Check liveness, which does not contact the collector:

```bash
curl --unix-socket /run/opensvc-oc3-mcp/delegated.sock http://oc3-mcp/health
```
