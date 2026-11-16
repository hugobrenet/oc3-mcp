# Contract with the collector API

The collector API moves on, each collector at its own version. This directory
says which part of it oc3-mcp uses, and CI checks it against the API of the
day.

| File | What it is |
|---|---|
| `oc3-api.yaml` | The API of opensvc/oc3 this MCP follows, `server/api.yaml` at the commit of `oc3-api.ref` |
| `oc3-api.ref` | That commit |
| `routes.txt` | The operations the core calls, `METHOD /path`, as `oc3-api.yaml` names them |
| `check.sh` | Compares `oc3-api.yaml` with the API of oc3 at a branch or a tag, on those operations only |

## How they are kept

- **`routes.txt` comes from the tests, not by hand.** The tests of
  `internal/core` record every request the core sends its fake collector,
  and map it to an operation of `oc3-api.yaml`. A request no operation
  matches fails the tests; so does a `routes.txt` that is not those
  operations. After a change of the routes the core calls:

  ```sh
  OC3MCP_UPDATE_ROUTES=1 go test ./internal/core/
  ```

- **`check.sh` runs in the Contract workflow**, on every pull request, on
  `main` and daily, against the `main` branch of oc3, which fails the run,
  and against its latest release, which reports only. It fails on what
  `oasdiff breaking` calls an error for a client of those operations: an
  operation gone, a parameter now required, a response of another shape.

- **Following a change of oc3**: adapt the code and its tests, then take its
  API as the new reference, in the same pull request:

  ```sh
  ref=$(gh api repos/opensvc/oc3/commits/main -q .sha)
  curl -fsSL -o contract/oc3-api.yaml "https://raw.githubusercontent.com/opensvc/oc3/$ref/server/api.yaml"
  echo "$ref" > contract/oc3-api.ref
  ```

## What it does not catch

It compares API descriptions. A requirement of the MCP the API never made,
such as a field the API declares optional and the MCP read as always there,
does not show: the tests and the reading of the MCP have to follow the API
as it is declared.
