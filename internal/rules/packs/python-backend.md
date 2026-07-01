# Python backend rules

## Language
- Flag mutable default arguments in function signatures.
- Flag `pickle` or `yaml.load` (without SafeLoader) on external input.
- Flag non-atomic ORM counter updates — use `select_for_update`/`F()` expressions.

## Django / ORM
- Flag `mark_safe`/`|safe` on user-controlled data (XSS).
- Flag disabled CSRF protection without justification.
- Flag N+1 query patterns: iterating a queryset and touching FK/M2M attributes without `select_related`/`prefetch_related`.
- Flag unbounded querysets returned to serializers or templates — require pagination or explicit limits on list endpoints.
- Flag queries inside loops that could be a single `filter(...in=...)`, `bulk_create`, or `bulk_update`.
- Flag model/schema changes that lack a corresponding migration mention, and destructive migrations (dropping columns/tables) without a rollout note.
- Flag `objects.get(...)` without handling `DoesNotExist`/`MultipleObjectsReturned` where the lookup can miss.
- Flag transactions missing where multiple related writes must succeed or fail together (`transaction.atomic`).

## FastAPI
- Flag blocking calls inside `async def` endpoints (`requests`, `time.sleep`, sync DB sessions/drivers) — they stall the event loop; use an async client/driver or a plain `def` endpoint (runs in the threadpool).
- Flag mixing a sync SQLAlchemy session into `async def` handlers — use the async session or sync endpoints consistently.
- Flag endpoints returning ORM/model objects directly without a `response_model` or explicit schema — leaks fields added later.
- Flag request bodies or query/path params without Pydantic validation or constraints where invariants matter (ranges, lengths, enums).
- Flag error paths raising bare exceptions instead of `HTTPException` (or a registered exception handler) — clients get opaque 500s.
- Flag per-request construction of expensive objects (HTTP clients, DB engines) instead of a shared dependency or lifespan state.
- Flag DB sessions not managed via a `yield` dependency with close/rollback — leaks connections on exceptions.
- Flag CPU-heavy or long-running work in `BackgroundTasks` — it runs in the same process; use a task queue.
- Flag mutable module-level state used as app state across workers — each worker has its own copy; use a store (DB/Redis).

## Flask
- Flag `app.run(debug=True)` or `DEBUG = True` reachable in production code paths.
- Flag mutating routes missing auth checks (`@login_required` or equivalent).
- Flag sensitive data stored in the client-side session cookie — it is signed, not encrypted; readable by the user.
- Flag `request.args`/`request.form`/`request.json` values used without validation or type coercion (trust boundary).
- Flag file responses built from user input (`send_file`, `os.path.join` with request data) — path traversal; use `send_from_directory` or sanitize.
- Flag missing rollback/cleanup of DB sessions on error paths (no `teardown_appcontext`/`session.remove`, commit without rollback handling).
- Flag mutable global variables used for per-request or cross-request state — breaks under multiple workers; use `g` for request scope, a store for shared state.
- Flag error handlers (or their absence) that return HTML/inconsistent shapes on JSON APIs — register handlers returning the API's error schema.

## Celery / background work
- Flag blocking work in request handlers that belongs in a task queue.
- Flag new tasks without an explicit retry policy where the work can fail transiently.
