# Pull request workflow

Changes to `main` go through pull requests. GitHub does not run CI or require a status check. Run `./scripts/phase4-release-gate.sh` locally before opening or updating a pull request, then record the command and result in the pull request template.

The release gate is the source of truth for local verification. It runs Go tests and race tests, vet and builds, frontend tests and production builds, embedded-asset checks, and deployment integration tests. Install the Go version from `.mise.toml`, Node.js 22, and `zsh` first.

The `main` branch rule requires a pull request, but no review approval or status check. It also blocks direct pushes, force pushes, and branch deletion, including for administrators. GitHub cannot verify a claimed local result, so reviewers should treat the verification section as evidence supplied by the author rather than an automated check.
