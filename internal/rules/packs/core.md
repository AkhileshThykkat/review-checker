# Generic review rules

## Correctness
- Flag logic that fails on realistic inputs: null/undefined/None, empty collections, missing keys, type mismatches, off-by-one.
- Flag error handling that swallows failures silently (empty catch blocks, broad catch-alls without logging or re-raising).
- Flag race conditions: check-then-act on shared state, non-atomic read-modify-write on rows or counters.

## Security
- Flag any raw SQL built with string formatting or interpolation — parameterize instead (SQL injection).
- Flag user input rendered without escaping (XSS).
- Flag secrets, API keys, tokens, or passwords hardcoded in source or logged.
- Flag missing authentication/permission checks on endpoints that mutate data.
- Flag dynamic code execution (`eval`, `exec`, `Function(...)`) on external input.

## API design
- Flag endpoints returning inconsistent shapes between success and error paths.
- Flag missing input validation on request bodies and query params (trust boundary).
- Flag breaking changes to existing public API response fields without versioning.

## Reliability & performance
- Flag blocking calls (HTTP requests, heavy queries) inside request handlers that should be async/queued.
- Flag missing timeouts on outbound HTTP calls.
- Flag caching of user-specific data under a shared cache key.
- Flag unbounded memory patterns: reading whole files/result sets into memory when streaming/iterating is available.

## Maintainability
- Flag dead code, commented-out blocks, and debug leftovers (`print`, `pdb`, `console.log`, `debugger`).
- Flag duplicated non-trivial logic that should reuse an existing helper visible in the diff.
- Flag misleading names: functions whose name contradicts what the body does.
- Only comment on style when it changes meaning or hides a bug — formatters own style.
