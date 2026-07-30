#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only

set -exo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC2250,SC1091
source "${script_dir}/common.sh"

GEL_REPO="us-docker.pkg.dev/grafanalabs-global/docker-enterprise-logs-prod/enterprise-logs"
LOKI_REPO="us-docker.pkg.dev/grafanalabs-global/dockerhub-loki-prod-mirror/loki"

install_crane() {
  local version="v0.21.6"
  local tmp
  tmp="$(mktemp -d)"
  curl -fsSL "https://github.com/google/go-containerregistry/releases/download/${version}/go-containerregistry_Linux_x86_64.tar.gz" \
    | tar -xzf - -C "${tmp}" crane
  export PATH="${tmp}:${PATH}"
}

if ! command -v crane &>/dev/null; then
  install_crane
fi

# Authenticate crane against GAR. login-to-gar configures the gcloud Docker credential
# helper; crane needs an explicit login for reliable non-interactive use.
crane auth login us-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"

# Uses GAR image tags to figure out what is the latest weekly image tag.
# Weekly tags are k317-195b91a (Loki) and weekly-k317-1a2b3c4 (GEL). The anchored
# regex rejects branch builds such as k316-compaction-f35210d-WIP and main-<sha>.
find_latest_image_tag() {
  local repo=$1
  local tags
  tags="$(crane ls "${repo}" | grep -P '^(weekly-)?k\d+-[0-9a-f]{7}$' || true)"
  if [[ -z "${tags}" ]]; then
    echo "No weekly image tags found in ${repo}" >&2
    exit 1
  fi
  awk '{ k=$0; sub(/^weekly-/,"",k); sub(/^k/,"",k); sub(/-.*/,"",k); print k"\t"$0 }' <<<"${tags}" \
    | sort -k1,1nr | head -1 | cut -f2
}

# takes k197-abcdef and returns k197, weekly-k197-abcdef and returns k197
extract_k_version() {
  sed -E "s/^(weekly-)?(k[0-9]+).*/\2/" <<<"$1"
}

calculate_next_chart_version() {
  local current_chart_version=$1
  local latest_image_tag=$2
  local k_release=$3

  local current_chart_semver
  current_chart_semver=$(echo "${current_chart_version}" | grep -P -o '^(\d+.){2}\d+')
  local new_chart_weekly
  new_chart_weekly=$(extract_k_version "${latest_image_tag}" | grep -P -o '\d+')
  local new_chart_semver="${current_chart_semver}"
  if [[ "${current_chart_version}" != *weekly* ]]; then
    # If previous version was not a weekly, then it was a stable release.
    # _This_ weekly release should have a semver that's one above the stable release.
    new_chart_semver=$(increment_semver "${current_chart_semver}" 1)
    # Also reset the patch release number to 0.
    new_chart_semver=$(set_semver_patch_to_zero "${new_chart_semver}")
  fi

  if ${k_release}; then
    echo "${new_chart_semver}-weekly.${new_chart_weekly}"
  else
    echo "${new_chart_semver}"
  fi
}

validate_version_update() {
  local new_chart_version=$1
  local current_chart_version=$2
  local latest_gel_tag=$3
  local latest_loki_tag=$4

  if [[ "${new_chart_version}" == "${current_chart_version}" ]]; then
    echo "New chart version (${new_chart_version}) is the same as current version (${current_chart_version}); not submitting weekly PR"
    exit 1
  fi

  local gel_weekly_version
  gel_weekly_version=$(extract_k_version "${latest_gel_tag}")
  local loki_weekly_version
  loki_weekly_version=$(extract_k_version "${latest_loki_tag}")
  echo "Comparing GEL weekly version (${gel_weekly_version}) with Loki weekly version (${loki_weekly_version})"
  if [[ "${gel_weekly_version}" != "${loki_weekly_version}" ]]; then
    echo "GEL weekly version (${gel_weekly_version}) does not match Loki weekly version (${loki_weekly_version}); not submitting PR"
    exit 1
  fi
}

k_release=false
if [[ "$1" == "-k" ]]; then
  k_release=true
  shift
fi

values_file=production/helm/loki/values.yaml
chart_file=production/helm/loki/Chart.yaml

latest_loki_tag=$(find_latest_image_tag "${LOKI_REPO}")
latest_gel_tag=$(find_latest_image_tag "${GEL_REPO}")
current_chart_version=$(get_yaml_node "${chart_file}" .version)
new_chart_version=$(calculate_next_chart_version "${current_chart_version}" "${latest_loki_tag}" "${k_release}")

validate_version_update "${new_chart_version}" "${current_chart_version}" "${latest_gel_tag}" "${latest_loki_tag}"

if ${k_release}; then
  update_yaml_node "${values_file}" .loki.image.tag "${latest_loki_tag}"
  update_yaml_node "${values_file}" .lokiCanary.image.tag "${latest_loki_tag}"

  update_yaml_node "${values_file}" .enterprise.image.tag "${latest_gel_tag}"
  update_yaml_node "${values_file}" .enterprise.provisioner.image.tag "${latest_gel_tag}"

  update_yaml_node "${chart_file}" .appVersion "$(extract_k_version "${latest_loki_tag}")"
fi

update_yaml_node "${chart_file}" .version "${new_chart_version}"

if ${k_release}; then
  sed --in-place \
    --regexp-extended \
    "s/## Unreleased/## Unreleased\n\n## ${new_chart_version}\n\n- \[CHANGE\] Changed version of Grafana Loki to ${latest_loki_tag}\n- \[CHANGE\] Changed version of Grafana Enterprise Logs to ${latest_gel_tag}/g" production/helm/loki/CHANGELOG.md
else
  sed --in-place \
    --regexp-extended \
    "s/## Unreleased/## Unreleased\n\n## ${new_chart_version}/g" production/helm/loki/CHANGELOG.md
fi

# shellcheck disable=SC2154,SC2250
echo "new_chart_version=${new_chart_version}" >> "$GITHUB_OUTPUT"
if ${k_release}; then
  # shellcheck disable=SC2154,SC2250
  echo "weekly=$(extract_k_version "${latest_loki_tag}")" >> "$GITHUB_OUTPUT"
fi
