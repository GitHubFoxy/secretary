# Remove code comments and prohibit new ones

Type: task
Status: ready-for-agent

## Goal

Remove human-authored comments from first-party code files in Secretary v2 and add a rule to the root `AGENTS.md` prohibiting new code comments. The reason is that comments can become outdated, preserve obsolete assumptions, and mislead decisions about the implementation.

## Scope

- Inventory comments in tracked first-party source files, tests, and scripts.
- Remove explanatory, descriptive, and rationale comments from those code files, including doc comments and inline comments.
- Add a clear rule to the root `AGENTS.md`: do not write comments in code files.
- Keep documentation prose in Markdown and other documentation files; this issue does not remove comments from documentation.
- Do not mistake comment-like text in strings, test fixtures, URLs, or data for code comments.
- Preserve compiler/toolchain directives whose comment syntax is required (for example, Go's `//go:embed`) unless equivalent behavior replaces them. The `AGENTS.md` rule should make this narrow exception explicit so an agent does not break builds while enforcing the policy.

## Acceptance

- No human-authored comments remain in tracked first-party code files within scope.
- Any required compiler/toolchain directives that remain are inventoried and have no explanatory prose attached.
- The root `AGENTS.md` explicitly prohibits writing comments in code files and clarifies the required-directive exception.
- Formatting and the relevant test/build suite pass; the cleanup does not change runtime behavior.

## Rationale

Comments can drift from the code they describe. Removing them makes implementation behavior the source of truth and avoids decisions based on stale commentary.

## Related

See [the code comment policy spec](../spec.md).
