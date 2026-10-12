# Configuration

[Back to README](../README.md)

## Environment variables

| Variable | Default | Description |
|---|---|---|
| OPENSVC_MCP_LISTEN_ADDR | 127.0.0.1:8444 | HTTPS bind address, loopback IPv4:port or [IPv6]:port |
| OPENSVC_MCP_TLS_CERT_FILE | required | Absolute server certificate PEM path |
| OPENSVC_MCP_TLS_KEY_FILE | required | Absolute server private key PEM path |
| OPENSVC_MCP_COLLECTOR_URL | required | Base URL of the collector API, HTTPS, for example `https://collector.example.com/api` |
| OPENSVC_MCP_COLLECTOR_CA_FILE | empty | Optional absolute PEM trust bundle for the collector; replaces system roots |
| OPENSVC_MCP_COLLECTOR_CREDENTIALS_FILE | required | Absolute path of the netrc file holding the collector account |

The default port differs from the 8443 of `om3-mcp`, so both MCP servers can
run on the same host. The configuration and the credentials are read at startup; restart after a
change. Startup does not contact the collector.

## Collector account

The MCP reads the collector with one account, sent with HTTP Basic
authentication to the collector origin only. Store it in a netrc file owned by
the MCP runtime account, mode `0600` or `0400`; a file readable by its group or
others is refused:

```bash
install -m 600 /dev/null /etc/opensvc-oc3-mcp/collector.netrc
```

```text
machine collector.example.com login <login> password <password>
```

The entry matches the host of `OPENSVC_MCP_COLLECTOR_URL`; a `default` entry
is used when no machine matches. Do not put the account in Git, the
environment or command-line arguments.

Every MCP caller sees what this account sees in the collector: its groups set
the visible nodes and services. The MCP does not authenticate its clients yet,
so it only listens on a loopback address. Give the account read access only.

## Collector connection

Requests use TLS 1.2 or later, no proxy and no redirect, with a 30-second
timeout. Responses are bounded to 10 MiB. A collector error is returned with
its HTTP status and the bounded collector message; a refused account is
reported without the collector's details.
