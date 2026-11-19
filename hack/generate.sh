#!/usr/bin/env bash
# Repo-owned replacement for the submodule-provided `task generate`.
#
# The renovate-generate workflow runs on every push to renovate/** — i.e.
# before review or merge — and executes whatever generation logic the
# hack/common submodule bump carries. This script ports the subset of
# hack/common/tasks_gen.yaml that this repo's `generate` task actually runs,
# so generation only executes reviewed code from this repo. The root
# Taskfile's explicit `generate` task calls this script and thereby replaces
# the submodule-provided task everywhere (local runs, gating CI,
# renovate-generate).
#
# Skipped vs. the original: `command-reference` (CLI repos only) and `docs`
# (only when GENERATE_DOCS_INDEX=true, unset here).
set -euo pipefail
cd "$(dirname "$0")/.."

# renovate: datasource=github-releases depName=kubernetes-sigs/controller-tools
CONTROLLER_GEN_VERSION=v0.23.0
# renovate: datasource=github-tags depName=golang/tools
GOIMPORTS_VERSION=v0.52.0

# Mirrors the configuration block of Taskfile.yaml — keep in sync.
NESTED_MODULES=(api)
API_DIRS=(api/v1alpha1/...)
MANIFEST_OUT=api/crds/manifests
CODE_DIRS="cmd internal api/v1alpha1"

MODULE_NAME=$(grep -E '^module[[:space:]]' go.mod | awk '{print $2}')
LOCALBIN=bin
mkdir -p "$LOCALBIN"

# sed -i that works on both GNU and BSD sed.
sedi() {
  local file="${@: -1}"
  [[ -f "$file" ]] || return 0
  local tmp
  tmp=$(mktemp)
  sed "${@:1:$#-1}" "$file" > "$tmp" && mv "$tmp" "$file"
}

ensure_tool() {
  local name="$1" pkg="$2" ver="$3"
  if [[ ! -x "$LOCALBIN/$name" ]] || [[ "$(cat "$LOCALBIN/.$name.version" 2>/dev/null)" != "$ver" ]]; then
    GOBIN="$PWD/$LOCALBIN" go install "$pkg@$ver"
    echo -n "$ver" > "$LOCALBIN/.$name.version"
  fi
}

ensure_tool controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen "$CONTROLLER_GEN_VERSION"
ensure_tool goimports golang.org/x/tools/cmd/goimports "$GOIMPORTS_VERSION"

# --- tidy -------------------------------------------------------------------
GO_VERSION=$(grep -E '^go [0-9]+\.[0-9]+\.[0-9]+' go.mod | awk '{print $2}')
for module in "${NESTED_MODULES[@]}"; do
  sedi -E "s/go [0-9]+\.[0-9]+\.[0-9]+/go ${GO_VERSION}/" "$module/go.mod"
  (cd "$module" && go mod tidy)
done
go mod tidy
sedi -E "s/toolchain go[0-9]+\.[0-9]+\.[0-9]+/toolchain go${GO_VERSION}/" go.mod
for module in "${NESTED_MODULES[@]}"; do
  sedi -E "s/toolchain go[0-9]+\.[0-9]+\.[0-9]+/toolchain go${GO_VERSION}/" "$module/go.mod"
done

# NOTE: controller-gen ignores relative '...' package paths unless they
# start with './' — and fails silently. Hence the guard after the manifest
# step below.

# --- code generation (deepcopy etc.) -----------------------------------------
for dir in "${API_DIRS[@]}"; do
  "$LOCALBIN/controller-gen" object paths="./$dir"
done

# --- CRD manifests ------------------------------------------------------------
rm -rf "$MANIFEST_OUT"
for dir in "${API_DIRS[@]}"; do
  "$LOCALBIN/controller-gen" \
    crd:allowDangerousTypes=true,generateEmbeddedObjectMeta=true \
    paths="./$dir" \
    output:crd:artifacts:config="$MANIFEST_OUT"
done

test -n "$(ls -A "$MANIFEST_OUT")" || { echo "error: no CRD manifests generated in $MANIFEST_OUT" >&2; exit 1; }

# --- formatting ---------------------------------------------------------------
go fmt ./...
for module in "${NESTED_MODULES[@]}"; do
  (cd "$module" && go fmt ./...)
done
"$LOCALBIN/goimports" -l -w -local="$MODULE_NAME" $CODE_DIRS
