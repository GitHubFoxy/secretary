# Continuous Integration (CI)

GitHub Actions runs the existing Phase 4 release gate on pushes to `main` and pull requests targeting `main`. The workflow is `.github/workflows/ci.yml`; `scripts/phase4-release-gate.sh` is the source of truth for the checks it runs.

The workflow uses Go from `.mise.toml`, Node.js 22, and `zsh`. It sets `umask 0022` to match the release-gate environment and checks the pull request diff against its base branch.

Run the same gate locally with `./scripts/phase4-release-gate.sh` after installing the versions declared by `.mise.toml` and Node.js 22.

## Branch Protection

`main` does not yet require pull requests or a successful status check. Enable these rules after the workflow completes on GitHub and confirms the exact required check name. This avoids configuring an unverified check that could block merges.
