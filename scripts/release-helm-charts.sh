#!/usr/bin/env bash
# Helm chart release automation script
#
# Usage:
#   ./scripts/release-helm-charts.sh <version>
#
# Example:
#   ./scripts/release-helm-charts.sh 4.4.1
#
# What it does:
#   1. Extracts helm-charts/ from the main repo at the given tag (non-destructive)
#   2. Packages each chart into a temp dir
#   3. Runs `helm repo index <tempdir> --merge <committed-index>` so that only
#      new entries get fresh timestamps; all pre-existing entries are taken
#      verbatim from the committed index.yaml
#   4. Moves the new .tgz files and updated index.yaml into this directory

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

HELM_REPO_URL="https://aerospike.github.io/aerospike-kubernetes-operator/"

CHART_NAMES=(
    aerospike-kubernetes-operator
    aerospike-cluster
    aerospike-backup
    aerospike-backup-service
    aerospike-restore
)

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------

usage() {
    echo "Usage: $0 <version>"
    echo ""
    echo "  version   Chart version to release (e.g. 4.4.1)"
    echo ""
    echo "Examples:"
    echo "  $0 4.4.1"
    exit 1
}

if [[ $# -lt 1 ]]; then
    usage
fi

VERSION="$1"
TAG="${VERSION}"

# ---------------------------------------------------------------------------
# Path resolution
# ---------------------------------------------------------------------------

# Directory that holds the packaged .tgz files and index.yaml (this worktree)
PACKAGES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Resolve the main repo from the worktree's git metadata
GIT_COMMON_DIR="$(git rev-parse --git-common-dir 2>/dev/null)"
MAIN_REPO_DIR="$(dirname "${GIT_COMMON_DIR}")"

echo "==> Helm chart release: version=${VERSION}  tag=${TAG}"
echo "    Packages dir : ${PACKAGES_DIR}"
echo "    Main repo    : ${MAIN_REPO_DIR}"
echo ""

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

for tool in helm git; do
    if ! command -v "${tool}" &>/dev/null; then
        echo "Error: '${tool}' is required but not found in PATH"
        exit 1
    fi
done

if ! git -C "${MAIN_REPO_DIR}" rev-parse "${TAG}" &>/dev/null; then
    echo "Error: tag '${TAG}' not found in main repo (${MAIN_REPO_DIR})"
    echo "  Available recent tags:"
    git -C "${MAIN_REPO_DIR}" tag --sort=-version:refname | head -10 | sed 's/^/    /'
    exit 1
fi

# ---------------------------------------------------------------------------
# Temp workspace (cleaned up on exit)
# ---------------------------------------------------------------------------

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

EXTRACT_DIR="${WORK_DIR}/src"   # helm-charts/ extracted from the tag
NEW_PKGS_DIR="${WORK_DIR}/pkg"  # new .tgz packages + generated index.yaml
mkdir -p "${EXTRACT_DIR}" "${NEW_PKGS_DIR}"

# ---------------------------------------------------------------------------
# Step 1 — Extract helm-charts/ from the tagged commit (non-destructive)
# ---------------------------------------------------------------------------

echo "==> [1/4] Extracting helm-charts/ from tag ${TAG}..."

git -C "${MAIN_REPO_DIR}" archive "${TAG}" -- helm-charts/ \
    | tar -x -C "${EXTRACT_DIR}"

if [[ ! -d "${EXTRACT_DIR}/helm-charts" ]]; then
    echo "Error: helm-charts/ directory not found inside tag ${TAG}"
    exit 1
fi

echo "    Extracted to ${EXTRACT_DIR}/helm-charts/"

# ---------------------------------------------------------------------------
# Step 2 — Package each chart into the temp dir
# ---------------------------------------------------------------------------

echo ""
echo "==> [2/4] Packaging charts..."

packaged=()
for chart in "${CHART_NAMES[@]}"; do
    chart_dir="${EXTRACT_DIR}/helm-charts/${chart}"
    if [[ ! -d "${chart_dir}" ]]; then
        echo "    [skip] ${chart} — directory not found in tag"
        continue
    fi

    echo "    Packaging ${chart}..."
    helm package "${chart_dir}" --destination "${NEW_PKGS_DIR}"

    tgz_file=$(ls "${NEW_PKGS_DIR}/${chart}-"*.tgz 2>/dev/null | tail -1)
    if [[ -z "${tgz_file}" ]]; then
        echo "    Error: helm package did not produce a .tgz for ${chart}"
        exit 1
    fi

    packaged+=("$(basename "${tgz_file}")")
    echo "      -> $(basename "${tgz_file}")"
done

if [[ ${#packaged[@]} -eq 0 ]]; then
    echo "Error: no charts were packaged"
    exit 1
fi

echo "    Packaged ${#packaged[@]} chart(s)"

# ---------------------------------------------------------------------------
# Step 3 — Generate index.yaml for new packages, merging with committed index
#
# Running helm repo index on a directory that contains ONLY the new .tgz files
# means helm generates fresh entries only for those. --merge then pulls in all
# pre-existing entries verbatim (including their original `created` timestamps).
# ---------------------------------------------------------------------------

echo ""
echo "==> [3/4] Running helm repo index..."

MERGE_FLAG=()
if git show HEAD:index.yaml > "${WORK_DIR}/index.yaml.committed" 2>/dev/null; then
    echo "    Using committed index.yaml as merge base"
    MERGE_FLAG=(--merge "${WORK_DIR}/index.yaml.committed")
else
    echo "    No committed index.yaml found — creating fresh index"
fi

helm repo index "${NEW_PKGS_DIR}" --url "${HELM_REPO_URL}" "${MERGE_FLAG[@]}"
echo "    index.yaml generated"

# ---------------------------------------------------------------------------
# Step 4 — Move new packages and updated index.yaml into the packages dir
# ---------------------------------------------------------------------------

echo ""
echo "==> [4/4] Moving packages and index.yaml into ${PACKAGES_DIR}..."

for pkg in "${packaged[@]}"; do
    mv "${NEW_PKGS_DIR}/${pkg}" "${PACKAGES_DIR}/"
    echo "    + ${pkg}"
done

mv "${NEW_PKGS_DIR}/index.yaml" "${PACKAGES_DIR}/index.yaml"
echo "    + index.yaml"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

echo ""
echo "==> Release preparation complete!"
echo ""
echo "    Review changes:"
echo "      git diff index.yaml"
echo "      git status"
echo ""
echo "    Commit when ready:"
echo "      git add $(IFS=' '; echo "${packaged[*]}") index.yaml"
echo "      git commit -m \"Helm charts for AKO ${VERSION} release\""
