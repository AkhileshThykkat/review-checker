# JavaScript / TypeScript rules

## Async correctness
- Flag floating promises: async calls whose result is neither awaited nor `.catch`-handled — rejections vanish silently.
- Flag `await` inside a loop where the iterations are independent — batch with `Promise.all`/`allSettled`.
- Flag async operations without cancellation or timeout where the caller can go away (`AbortController` on fetch, cleared timers).
- Flag `Promise.all` where one rejection should not discard the other results — use `allSettled`.

## Type safety (trust boundaries)
- Flag `any`, `as any`, or `as unknown as T` used to type external data (API responses, JSON, form input) — parse/validate instead (e.g. zod) so the type matches runtime reality.
- Flag non-null assertions (`x!`) on values that can genuinely be null/undefined at runtime.
- Flag `@ts-ignore`/`@ts-expect-error` without a comment justifying why the error is safe to silence.
- Flag `JSON.parse` on external input without error handling or shape validation.

## Language pitfalls
- Flag `||` used for defaults where `0`, `''`, or `false` are valid values — use `??`.
- Flag loose equality (`==`) where coercion can change the result.
- Flag in-place mutation of arrays/objects the caller still holds (`sort`, `reverse`, `splice`, property assignment on parameters) — copy first when ownership is unclear.
- Flag optional chaining that silently skips required behavior (`config?.callback?.()` where the callback must run).

## Node / bundling
- Flag secrets or private API keys in code that ships to the client bundle (including `NEXT_PUBLIC_`/`VITE_`-prefixed env vars holding sensitive values).
- Flag outbound HTTP calls without a timeout or abort signal.
- Flag synchronous filesystem or CPU-heavy work on request-handling paths.
