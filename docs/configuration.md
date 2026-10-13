# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_DELEGATED_SOCKET | required | Absolute path of the Unix socket the MCP listens on, for example `/run/opensvc-oc3-mcp/delegated.sock` |
| OPENSVC_MCP_COLLECTOR_URL | required | Base URL of the collector API, HTTPS, for example `https://collector.example.com/api` |
| OPENSVC_MCP_COLLECTOR_CA_FILE | empty | Optional absolute PEM trust bundle for the collector; replaces system roots |

The configuration is read at startup; restart after a change. Startup does not
contact the collector.

## Authentication

The MCP holds no collector account and never receives a password. Every
request carries the OpenID Connect access token of its user, issued by the
identity provider the collector trusts:

```http
Authorization: Bearer <OpenID Connect access token>
```

The MCP checks the token shape before any collector call, without verifying
its signature: a JWT signed with an asymmetric algorithm, with a `kid`, and
`iss`, `sub`, `aud` and an unexpired `exp` claims. A missing, malformed or
expired token, a second `Authorization` header or a token in the query string
returns 401 without contacting the collector. The request ends when the token
expires.

The MCP forwards the token unchanged to the collector, which verifies its
signature, issuer, audience and expiry, resolves the account linked to its
issuer and subject, and applies its claim rules. Every caller therefore sees
what its own collector account sees. The token is sent to the collector
origin only, and never logged or returned.

### `GET /mcp/auth/whoami`

Proves the token by relaying it to the collector `GET /api/auth/whoami`. The
collector must authenticate it as a bearer token (`auth_source: bearer`) with
the token's own issuer and subject. The response is:

```json
{
  "issuer": "https://sso.example.com/application/o/collector/",
  "subject": "user-subject",
  "expires_at": "2026-10-13T12:00:00Z"
}
```

A token the collector refuses returns its 401 or 403; a token it
authenticates with another source or identity returns 401; any other collector
failure returns 502. Collector details are never returned.

## Socket access

The socket is the only listener. Access is controlled by the filesystem: the
MCP creates the socket with mode `0660`, owned by its runtime account and
group. Restrict the parent directory to the MCP account and a group shared
with the agent account, for example:

```bash
install -d -m 2750 -o opensvc-oc3-mcp -g opensvc-ai /run/opensvc-oc3-mcp
```

At startup, a socket file left by a stopped process is replaced. A socket with
a live listener, or any other file at the path, prevents startup.

Routes:

- `/mcp`: stateless Streamable HTTP, with the caller's token;
- `GET /mcp/auth/whoami`: the identity bridge above;
- `GET /health`: local liveness, without token or collector call.

## Collector connection

Requests use TLS 1.2 or later, no proxy and no redirect, with a 30-second
timeout. Responses are bounded to 10 MiB. A collector error is returned with
its HTTP status and the bounded collector message; a refused token is reported
without the collector's details.
