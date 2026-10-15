# Configuration

[Back to README](../README.md)

## Environment variables

The MCP has two listeners, at least one of which is configured: the Unix
socket of the AI agent, and HTTPS for external clients authenticated with
OAuth.

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_COLLECTOR_URL | required | Base URL of the collector API, HTTPS, for example `https://collector.example.com/api` |
| OPENSVC_MCP_COLLECTOR_CA_FILE | empty | Optional absolute PEM trust bundle for the collector; replaces system roots |
| OPENSVC_MCP_DELEGATED_SOCKET | empty | Absolute path of the Unix socket, for example `/run/opensvc-oc3-mcp/delegated.sock`; enables the socket listener |
| OPENSVC_MCP_DELEGATED_ISSUER | required with the socket | Issuer of the OpenID provider of the collector, copied from the collector `/api/auth/info`: the tokens of the socket are verified for it |
| OPENSVC_MCP_DELEGATED_AUDIENCE | OPENSVC_MCP_COLLECTOR_AUDIENCE with HTTPS and the token exchange; else required with the socket | Client ID of the collector, the audience of the tokens of the socket |
| OPENSVC_MCP_TLS_CERT_FILE, OPENSVC_MCP_TLS_KEY_FILE | empty | Absolute PEM certificate and private key; enable the HTTPS listener |
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8444 | HTTPS bind address, explicit IP:port |
| OPENSVC_MCP_OAUTH_RESOURCE_URL | required with HTTPS | Public URL of the MCP, ending in `/mcp`: the audience of the incoming tokens |
| OPENSVC_MCP_OAUTH_ISSUER | required with HTTPS | Issuer of the incoming tokens, the public OAuth client of the MCP |
| OPENSVC_MCP_OAUTH_RESOURCE_NAME | OpenSVC Collector MCP | Name published in the resource metadata |
| OPENSVC_MCP_OAUTH_CA_FILE | empty | Optional absolute PEM trust bundle for the issuer |
| OPENSVC_MCP_DELEGATED_CA_FILE | empty | Optional absolute PEM trust bundle for the OpenID provider of the collector of the Unix socket, replacing the system roots; without it, the socket uses `OPENSVC_MCP_OAUTH_CA_FILE` when the HTTPS listener has one, else the system roots. Requires the socket |
| OPENSVC_MCP_AUTH_CONFIG_FILE | empty | Exchange profiles, see [token exchange](#token-exchange) |
| OPENSVC_MCP_COLLECTOR_AUTH_PROFILE | empty | The profile of OPENSVC_MCP_AUTH_CONFIG_FILE that exchanges the tokens |
| OPENSVC_MCP_COLLECTOR_AUDIENCE | empty | The audience requested for the collector: the client ID the collector trusts |
| OPENSVC_MCP_MAX_CONCURRENT_CALLS | 10 | Tool calls in progress of one identity on HTTPS; 0 disables the limit. See [call limits](#call-limits) |
| OPENSVC_MCP_MAX_CALLS_PER_MINUTE | 120 | Tool calls per minute of one identity on HTTPS, with a burst of 30; 0 disables the limit |

The three token exchange settings go together. Without them, the HTTPS
listener serves the tool list but refuses the tool calls. The configuration is
read at startup; restart after a change. Startup contacts neither the
collector nor the identity provider.

## HTTPS for external clients

The HTTPS listener serves external MCP clients, such as coding agents, which
sign in at the identity provider with the public OAuth client of the MCP.

- `/mcp` requires `Authorization: Bearer <access token>`. The MCP verifies the
  token itself: its signature with the keys the issuer publishes, the issuer,
  the audience, which must contain `OPENSVC_MCP_OAUTH_RESOURCE_URL`, and its
  dates. A missing or invalid token gets 401, with a `WWW-Authenticate`
  challenge naming the resource metadata; an unreachable issuer gets 503.
- `/.well-known/oauth-protected-resource` and
  `/.well-known/oauth-protected-resource/mcp` publish the resource metadata
  (RFC 9728): the resource URL and its authorization server.
- `/health` answers without authentication.

On both listeners, HTTP headers are bounded to 64 KiB and request bodies to
1 MiB; the read of the headers is bounded to 5 seconds and the read of a whole
request, its body included, to 10 seconds, so that a client sending it slowly
is cut off. The processing and the response of a request are bounded to 3
minutes, above the longest tool (a token exchange of up to the 1-minute
maximum `request_timeout`, then collector requests of up to 30 seconds each,
two for `get_service`), so that a client reading its response slowly is cut
off too.

The token of an external client is never sent to the collector: it is
intended for the MCP. Each tool call exchanges it for a token intended for the
collector.

### Call limits

The tool calls of each identity on HTTPS (token issuer and subject) are
limited: `OPENSVC_MCP_MAX_CONCURRENT_CALLS` calls in progress and
`OPENSVC_MCP_MAX_CALLS_PER_MINUTE` calls per minute, with a burst of 30 calls
for an idle identity. A call beyond a limit fails at once, before any token
exchange or collector request, with a tool error that tells the client when to retry;
the audit line records it as `outcome=tool_error`. The limits protect the SSO
and the collector from an authenticated client that loops, such as an agent repeating
a call. They are not a network protection: limits per address, before
authentication or for all clients belong to the reverse proxy, gateway or
firewall in front of the MCP, where every request otherwise carries the address
of the proxy. The Unix socket is not limited.

The limits are per identity: more users do not share them, but an identity
shared by several people or an automation does. Questions over a large fleet
should use the list tools, which return many items per call, rather than one
call per node or service; raise `OPENSVC_MCP_MAX_CALLS_PER_MINUTE` if a
legitimate use still exceeds it. Size the SSO and the collector for the total
load of all users, which the limits do not bound.

### Token exchange

On each `tools/call`, the MCP exchanges the token of the client (RFC 8693) at
the token endpoint of the exchange profile issuer, with the audience
`OPENSVC_MCP_COLLECTOR_AUDIENCE`, and sends the token it receives to the
collector, which verifies it and applies the account and the claim rules of
the user. The exchanged token must be a JWT for that audience, and must not
be usable as an MCP token.

An exchanged token is kept in the memory of the MCP process, never on disk,
and reused by the next tool calls of the same MCP token for the same target:
for at most one minute, and never within 30 seconds of its expiry. The cache
key is a hash of the MCP token and the target, never the MCP token itself.
Concurrent calls share one exchange. A failed exchange is returned again for
five seconds without contacting the SSO. When the target
refuses the token (HTTP 401 or 403), it is forgotten and the next call
exchanges again. A claim change in the SSO therefore applies within a minute.

`OPENSVC_MCP_AUTH_CONFIG_FILE` holds the exchange profiles, owned by the MCP
runtime account; the client secret stays in its own file, mode `0600` or
`0400`:

```yaml
version: 1
profiles:
  collector-sso:
    issuer: https://sso.example.com/application/o/oc3-mcp-exchange/
    client_id: oc3-mcp-exchange
    client_secret_file: /etc/opensvc-oc3-mcp/secrets/oc3-mcp-exchange
    token_endpoint_auth_method: client_secret_basic
    scopes: [openid, email, profile, collector]
    request_timeout: 10s
```

The scopes are those of the collector provider, so that the exchanged token
carries the claims the collector rules test: here `collector`, the scope of
the claims mapping, for example `collector_access`. The collector refuses a
token no claim rule allows with 401, without detail.

The identity provider needs three clients:

| Client | Role |
|---|---|
| Public client of the MCP, for example `oc3-mcp` | Sign-in of the external clients with PKCE; its access tokens carry the MCP resource URL as audience |
| Confidential exchange client, for example `oc3-mcp-exchange` | Exchanges the tokens issued for the public client |
| The collector application | Issues the exchanged tokens for its audience; they must carry the claims the collector rules require to allow access |

Error messages of a refused exchange name the provider error code only, never
its description.

## Unix socket authentication

The MCP holds no collector account and never receives a password. Every
request on the socket carries the OpenID Connect access token of its user,
issued by the identity provider the collector trusts:

```http
Authorization: Bearer <OpenID Connect access token>
```

The MCP verifies the token locally before any collector request: a JWT
signed with an asymmetric algorithm, with a `kid`, by a key of the OpenID
provider of the collector, `OPENSVC_MCP_DELEGATED_ISSUER` as `iss`,
`OPENSVC_MCP_DELEGATED_AUDIENCE` in `aud`, a `sub` and an unexpired `exp`;
`iat` and `nbf` tolerate 30 seconds of clock skew. A missing, malformed,
expired or refused token, a second `Authorization` header or a token in the
query string returns 401 and never reaches the collector. The request ends
when the token expires.

Both values follow the OpenID configuration of the collector: copy them from
the `oidc` object (`issuer`, `client_id`) of its public `GET /api/auth/info`.
The MCP does not discover them: when the collector changes them, update the
MCP, which refuses the tokens meanwhile. The keys of the issuer are discovered
and cached 5 minutes, trusting `OPENSVC_MCP_DELEGATED_CA_FILE`, else
`OPENSVC_MCP_OAUTH_CA_FILE`, else the system roots; an unknown key ID reloads
them at most every 30 seconds. When the keys cannot be loaded, the socket
returns 503 with `Retry-After`.

The MCP then sends the verified token to the collector, which verifies it
too, resolves the account linked to its issuer and subject, and applies its
claim rules. Every caller therefore sees what its own collector account sees.
The token is sent to the collector origin only, and never logged or returned.

### `GET /mcp/auth/whoami`

Proves the verified token by relaying it to the collector
`GET /api/auth/whoami`. The collector must authenticate it as a bearer token
(`auth_source: bearer`) with the token's own issuer and subject. The response is:

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

Access to the socket is controlled by the filesystem: the MCP creates the socket with mode `0660`, owned by its runtime account and
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

## Audit trail

Every `tools/call` emits one `INFO` line `mcp tool call` through the process
logger (standard error), for example:

```text
INFO mcp tool call tool=get_node issuer=https://sso.example.com/application/o/collector/ subject=<sub> client_id="" exchange=delegated collector_subject=<sub> outcome=ok duration=212ms request_id=<agent request ID>
```

| Field | Meaning |
|---|---|
| `tool` | Requested tool, bounded to 256 bytes |
| `issuer`, `subject` | The verified identity of the caller's token: the MCP token on HTTPS, the delegated token on the socket. A refused token gets 401 before any tool call, so it is never logged here |
| `client_id` | On HTTPS, the client application from the `client_id` or `azp` claim; empty on the socket |
| `exchange` | `delegated` on the socket; on HTTPS, `ok`, the error message of a refused exchange, or `none` when no exchange was attempted |
| `collector_subject` | The subject of the token the collector received, to correlate with its logs |
| `outcome`, `detail` | `ok`, `tool_error` (collector refusal such as HTTP 401 or 404, or validation error) or `error` (protocol error); `detail` is the bounded error text already returned to the client |
| `duration` | Total call duration, including the collector requests |
| `request_id` | On the socket, the `X-Request-ID` the AI agent sends, which its own audit lines carry; empty on HTTPS, or when absent or not 32 lowercase hexadecimal characters |

Every `GET /mcp/auth/whoami` emits one `INFO` line `mcp whoami`:

```text
INFO mcp whoami issuer=https://sso.example.com/application/o/collector/ subject=<sub> outcome=collector_refused status=401 duration=35ms collector_status=401
```

| Field | Meaning |
|---|---|
| `issuer`, `subject` | The verified identity of the caller's access token |
| `outcome` | `ok`; `collector_refused` (the collector answered 401 or 403); `collector_failed` (any other collector failure, answered 502); `identity_mismatch` (the collector authenticated another source or identity than this token); `expired`; `bad_request` |
| `status` | The HTTP status the bridge answered |
| `duration` | Total check duration, including the collector request |
| `collector_status`, `detail`, `auth_source` | When relevant: the collector HTTP status, the bounded error of a collector failure, the authentication source the collector reported |
| `request_id` | As for `mcp tool call` |

Tokens, raw arguments and tool results are never logged. Values are quoted by
the structured logger. Retention and shipping of these logs belong to the
deployment.

## Collector connection

Requests use TLS 1.2 or later, no proxy and no redirect, with a 30-second
timeout. Responses are bounded to 10 MiB. A collector error is returned with
its HTTP status and the bounded collector message; a token the collector
refuses with 401 is reported without the collector's details.
