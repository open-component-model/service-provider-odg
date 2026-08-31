# Contributing to service-provider-odg

For the general contribution process (fork-and-pull workflow, commit requirements, DCO,
code of conduct, and more), see the
[OCM Contributing Guide](https://ocm.software/community/contributing/).

This document covers repository-specific setup and development workflows.

## Prerequisites

- **Go 1.26+**
- **[Task](https://taskfile.dev/) v3.x** - runs all build, test, and lint commands
- **Docker** - required for image builds and E2E tests
- **kubectl** - cluster interaction
- **[Kind](https://kind.sigs.k8s.io/)** - local cluster provisioning for E2E tests

## Getting Started

```bash
# Clone with submodules (hack/common is the shared build toolchain)
git clone --recurse-submodules https://github.com/open-component-model/service-provider-odg.git
cd service-provider-odg

# If already cloned without submodules
git submodule update --init --recursive
```

## Project Structure

```text
.
├── api/v1alpha1/          # CRD type definitions (ODG, ProviderConfig)
├── cmd/                   # Controller entrypoint
├── internal/controller/   # Reconciler implementations
├── test/e2e/              # End-to-end tests
├── hack/common/           # Shared build toolchain (git submodule -> openmcp-project/build)
├── Taskfile.yaml          # Project-specific task definitions
├── VERSION                # Release version (semver or semver-dev)
└── .github/workflows/     # CI/CD pipelines
```

## Common Tasks

```bash
# Generate code (deepcopy, CRDs, formatting)
PATH="$PWD/bin:$PATH" task generate

# Run linters and validation
PATH="$PWD/bin:$PATH" task validate

# Run unit tests
PATH="$PWD/bin:$PATH" task test

# Build container image for local platform
PATH="$PWD/bin:$PATH" task build:img:build

# Build image and run E2E tests
PATH="$PWD/bin:$PATH" task test-e2e
```

## Development Workflow

For the general fork-and-pull workflow, commit signing requirements, and DCO sign-off, refer to
the [OCM Contributing Guide](https://ocm.software/community/contributing/). The steps below cover
the repo-specific workflow after you have your local branch ready.

1. Run `task generate` after modifying API types in `api/v1alpha1/`
2. Run `task validate` to ensure lint and vet pass
3. Run `task test` for unit tests
4. Run `task test-e2e` for full end-to-end validation (see below)
5. Submit a pull request - CI runs the same checks automatically

### Pull Request Requirements

PR descriptions **must** include the following sections (enforced by CI via
[validate-pr-content](.github/workflows/validate-pr-content.yaml)):

~~~markdown
**What this PR does / why we need it**:

<your description>

**Release note**:
```other operator
<release note or NONE>
```
~~~

For additional requirements (conventional commits, DCO, commit signing, squash merging), see
the [OCM Contributing Guide](https://ocm.software/community/contributing/).

### E2E Tests

The E2E tests use [sigs.k8s.io/e2e-framework](https://github.com/kubernetes-sigs/e2e-framework)
and:

1. Build a container image for the controller
2. Bootstrap a local kind cluster with Flux and the OpenMCP operator
3. Deploy the service provider and verify reconciliation

## Further Reading

- [Service Provider Development Guide](https://openmcp-project.github.io/docs/developers/serviceprovider/service-providers) -
  design, development, testing, and deployment of OpenMCP service providers
- [General Controller Guidelines](https://openmcp-project.github.io/docs/developers/general) -
  operation annotations, status reporting, event filtering
- [OpenMCP Documentation](https://openmcp-project.github.io/docs/) -
  full platform documentation
- [openmcp-project/build](https://github.com/openmcp-project/build) -
  shared build toolchain (the `hack/common` submodule)
