# Release Process

This repository uses a fully automated release pipeline. Releases are triggered by updating
the `VERSION` file on `main`.

For the general contribution workflow (forking, branching, pull requests), see the
[OCM Contributing Guide](https://ocm.software/community/contributing/).

## How It Works

### 1. Prepare the Release

Create a PR that updates the `VERSION` file from `vX.Y.Z-dev` to `vX.Y.Z`.

### 2. Merge to Main

Once the PR is merged, the [Versioned Release](.github/workflows/release.yaml) workflow runs
automatically and:

1. Reads and validates the version from the `VERSION` file
2. Skips if the version contains `-dev` (normal state between releases)
3. Skips if the version tag already exists
4. Creates an annotated Git tag (`vX.Y.Z`) and pushes it
5. Tags nested Go modules (e.g., `api/vX.Y.Z`) for independent importability - configured
   via `NESTED_MODULES` in `Taskfile.yaml`
6. Generates a changelog from merged PRs
7. Creates a **draft** GitHub release with the changelog
8. Bumps `VERSION` to `vX.Y.Z-dev` to return to development mode

### 3. Publish Artifacts

The tag push triggers the [Publish](.github/workflows/publish.yaml) workflow, which:

1. Builds multi-platform container images via Docker Buildx
2. Pushes images to `ghcr.io/open-component-model/images/service-provider-odg`
3. Builds and pushes the OCM component

> [!NOTE]
> The Helm chart step (`build:helm:all`) is a no-op for this repository since
> `CHART_COMPONENTS` is empty and no `charts/` directory exists.

### 4. Finalize

- Review and publish the draft GitHub release

## Version Format

The `VERSION` file follows strict semver:

| State | Format | Example |
|-------|--------|---------|
| Released | `vMAJOR.MINOR.PATCH` | `v0.1.2` |
| Development | `vMAJOR.MINOR.PATCH-dev` | `v0.1.2-dev` |

Development versions (`-dev`) are never published - both the release and publish workflows
skip them.

## Release-Related Workflows

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| [Versioned Release](.github/workflows/release.yaml) | Push to main | Tag, changelog, GitHub release, dev-bump |
| [Publish](.github/workflows/publish.yaml) | Tag push (`v*`) | Build and push images and OCM components |

Both workflows, as well as the CI and validation workflows that
gate PRs before merge, use reusable workflows from
[openmcp-project/build](https://github.com/openmcp-project/build/tree/main/.github/workflows).
