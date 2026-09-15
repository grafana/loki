#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only

set -exo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC2250,SC1091
source "${script_dir}/common.sh"

calculate_next_chart_version() {
  local current_chart_version=$1

  local current_chart_semver
  current_chart_semver="$(echo "${current_chart_version}" | grep -P -o '^(\d+.){2}\d+')"
  local new_chart_semver="${current_chart_semver}"
  new_chart_semver=$(increment_semver "${current_chart_semver}" 1)
  new_chart_semver=$(set_semver_patch_to_zero "${new_chart_semver}")
  echo "${new_chart_semver}"
}

validate_version_update() {
  local new_chart_version=$1
  local current_chart_version=$2

  if [[ "${new_chart_version}" == "${current_chart_version}" ]]; then
    echo "New chart version (${new_chart_version}) is the same as current version (${current_chart_version}); not submitting PR"
    exit 1
  fi
}

# Guards against re-dispatching the same gel_version, which would otherwise bump the
# chart and add a changelog entry describing a GEL update that never happened.
validate_gel_update() {
  local values_file=$1
  local new_gel_version=$2

  local current_gel_version
  current_gel_version=$(get_yaml_node "${values_file}" .enterprise.version)
  local current_gel_image_tag
  current_gel_image_tag=$(get_yaml_node "${values_file}" .enterprise.image.tag)

  if [[ "${current_gel_version}" == "${new_gel_version}" && "${current_gel_image_tag}" == "${new_gel_version}" ]]; then
    echo "Chart already uses GEL version ${new_gel_version}; not submitting PR"
    exit 1
  fi
}

if [[ -z "${1:-}" ]]; then
  echo "usage: $0 <gel-version>" >&2
  echo "example: $0 v3.6.11" >&2
  exit 1
fi

# Chart 7.0.0+ is GEL-only. Loki and GEL versions have diverged (Loki on 3.7.x,
# GEL stays on 3.6.x), so this path is driven by a GEL release version.
latest_gel_tag=$(sed -E "s/^v//" <<<"$1")

values_file=production/helm/loki/values.yaml
chart_file=production/helm/loki/Chart.yaml

current_chart_version=$(get_yaml_node "${chart_file}" .version)
new_chart_version=$(calculate_next_chart_version "${current_chart_version}")

validate_gel_update "${values_file}" "${latest_gel_tag}"
validate_version_update "${new_chart_version}" "${current_chart_version}"

update_yaml_node "${values_file}" .enterprise.version "${latest_gel_tag}"
update_yaml_node "${values_file}" .enterprise.image.tag "${latest_gel_tag}"
update_yaml_node "${chart_file}" .appVersion "${latest_gel_tag}"
update_yaml_node "${chart_file}" .version "${new_chart_version}"

sed --in-place \
  --regexp-extended \
  "s/## Unreleased/## Unreleased\n\n## ${new_chart_version}\n\n- \[CHANGE\] Changed version of Grafana Enterprise Logs to ${latest_gel_tag} (updated \`enterprise.version\`, and \`enterprise.image.tag\`)./g" production/helm/loki/CHANGELOG.md

# shellcheck disable=SC2154,SC2250
echo "new_chart_version=${new_chart_version}" >> "$GITHUB_OUTPUT"
