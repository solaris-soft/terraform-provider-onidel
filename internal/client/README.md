# Onidel VM client

This package implements the contract in `IMPLEMENTATION.md` using the standard
library. It uses bearer authentication and defaults to
`https://api.cloud.onidel.com`. Use HTTPS in production; HTTP is supported for
local testing. Base URLs may include an API path prefix, but not credentials,
queries, fragments, or traversal segments. All redirects are rejected, including
same-origin redirects. Errors never include request/response bodies, tokens,
server reason phrases, or raw transport errors.

## Provisioning response limitation

The API specification documents `POST /vm` returning 201, but specifies neither
a response body nor a `Location` header. `CreateVM` therefore accepts:

- A direct VM JSON object with a UUID `id`, including an ID-only object. If
  optional fields have incompatible types, the valid ID is still returned.
- Otherwise, a `Location` resolving to the configured API origin and exactly
  `<base-path>/vm/<UUID>`. Credentials, queries, fragments, encoded paths,
  traversal segments, other origins, and other resources are rejected.

A valid body ID takes precedence over `Location`. The location is only parsed;
it is never fetched. Missing or untrustworthy identity produces an explicit
error. Creation may already have succeeded even when decoding, transport, or
cancellation fails. **Reconcile manually before retrying creation.** The client
never retries POST, lists VMs, or identifies a VM by its name.

`WaitVM` performs an immediate GET and polls every two seconds while status is
`building` or `restoring`. Only `active` succeeds; all other states return an
error. Callers must provide an overall context deadline. Individual requests
have a 30-second timeout and a 1 MiB response limit. Context cancellation is
preserved for `errors.Is` checks, including ambiguous creation failures.

Run tests without contacting the paid API:

```sh
go test ./internal/client
go vet ./internal/client
```
