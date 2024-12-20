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
  local latest_loki_tag=$3

  if [[ "${new_chart_version}" == "${current_chart_version}" ]]; then
    echo "New chart version (${new_chart_version}) is the same as current version (${current_chart_version}); not submitting PR"
    exit 1
  fi
}

latest_loki_tag=$(sed -E "s/v(.*)/\1/g" <<<"$1")

values_file=production/helm/loki/values.yaml
chart_file=production/helm/loki/Chart.yaml

current_chart_version=$(get_yaml_node "${chart_file}" .version)
new_chart_version=$(calculate_next_chart_version "${current_chart_version}")

validate_version_update "${new_chart_version}" "${current_chart_version}" "${latest_loki_tag}"

update_yaml_node "${values_file}" .loki.image.tag "${latest_loki_tag}"

update_yaml_node "${values_file}" .enterprise.image.tag "${latest_loki_tag}"
update_yaml_node "${chart_file}" .appVersion "${latest_loki_tag}"
update_yaml_node "${chart_file}" .version "${new_chart_version}"

sed --in-place \
  --regexp-extended \
  "s/(.*\<AUTOMATED_UPDATES_LOCATOR\>.*)/\1\n\n## ${new_chart_version}\n\n- \[CHANGE\] Changed version of Grafana Loki to ${latest_loki_tag}/g" production/helm/loki/CHANGELOG.md

make TTY='' helm-docs

# shellcheck disable=SC2154,SC2250
echo "new_chart_version=${new_chart_version}" >> "$GITHUB_OUTPUT"
