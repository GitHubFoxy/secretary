# Code comment policy

## Goal

Avoid stale or misleading commentary in code. Comments can outlive the implementation or preserve assumptions that no longer match current behavior, so the code itself should remain the source of truth.

## Policy

- Do not add explanatory, descriptive, or rationale comments to code files.
- Remove existing human-authored comments from first-party code files, including source, tests, and scripts.
- Keep prose documentation in Markdown and other documentation files; this policy applies to code files, not project documentation.
- A compiler or toolchain directive that must use comment syntax (for example, Go's `//go:embed`) is not explanatory commentary. Keep such directives unless equivalent behavior replaces them, and do not add any unnecessary comment text around them.
- Text inside string literals, test fixtures, or data that merely resembles comment syntax is not a code comment and must not be changed as part of this cleanup.
