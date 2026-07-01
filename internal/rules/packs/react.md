# React rules

## Hooks & state
- Flag `useEffect`/`useMemo`/`useCallback` with missing or wrong dependencies — stale closures; fix the deps or restructure, don't silence the lint rule.
- Flag state updates that derive from the previous value without the functional form (`setCount(c => c + 1)`) where updates can batch or race.
- Flag state that merely mirrors props or other state and is synced via an effect — derive it during render instead.
- Flag hooks called conditionally or inside loops.
- Flag effects without cleanup for what they start: event listeners, intervals/timeouts, subscriptions, in-flight fetches (abort or ignore-stale flag).
- Flag async effects that apply responses without guarding against out-of-order completion (race: fast navigation applies stale data).

## Rendering
- Flag `key={index}` on lists that can reorder, insert, or delete — breaks state and reconciliation.
- Flag component definitions inside another component's render body — remounts on every render, losing state.
- Flag expensive computation running on every render of a hot component without memoization.
- Flag controlled/uncontrolled input switching: `value` without `onChange`, or a value that can become `undefined` mid-lifetime.

## Security & accessibility
- Flag `dangerouslySetInnerHTML` with user-influenced data (XSS) — sanitize or render as text.
- Flag click handlers on non-interactive elements (`div`, `span`) without `role`, `tabIndex`, and a keyboard handler.
- Flag `img` without meaningful `alt`, form inputs without an associated label, and anchors used as buttons (or vice versa).

## Design system
- Flag hardcoded visual values in styles or class names — hex/rgb colors, pixel spacing, font sizes, shadows, z-index, breakpoints — where the codebase has design tokens (theme values, CSS variables, Tailwind scale); consume the token instead of restating the raw value.
- Flag arbitrary Tailwind values (`w-[13px]`, `text-[#ff0000]`, `z-[9999]`) when a scale value exists — one-off values fragment the system.
- Flag new sibling components that fork an existing one by style (`PrimaryButton`, `DangerButton`) — extend the existing component with a variant prop (CVA or equivalent) instead.
- Flag raw HTML primitives (`button`, `input`, `select`) styled from scratch when the diff or codebase imports a component library or internal design-system components — reuse or extend the system component.
- Flag inline `style={{...}}` objects that bypass the styling system used elsewhere in the file.
- Flag manual conditional class-string concatenation where the codebase uses a `cn`/`clsx`/`cva` pattern.
- Flag duplicated UI blocks in the diff that should be one reusable component — reuse before create.
