#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only

set -exo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
source "${script_dir}/common.sh"

# Uses docker hub image tags to figure out what is the latest image tag
find_latest_image_tag() {
  local docker_hub_repo=$1
  local regExp="^(k|weekly-k)\d+-[a-z0-9]+"
  echo $(crane ls "${docker_hub_repo}" | grep -P "${regExp}" | sed -E "s/([weekly-]*k[[:digit:]]*)-([^-]*).*/\1-\2/g" | uniq | sort -Vur | head -1)
}

# takes k197-abcdef and returns r197, k197-abcdef-arm64 and returns k197, weekly-k197-abcdef and returns k197
extract_k_version() {
  echo $(sed -E "s/[weekly-]*(k[[:digit:]]*).*/\1/g" <<<$1)
}

calculate_next_chart_version() {
  local current_chart_version=$1
  local latest_image_tag=$2

  local current_chart_semver=$(echo $current_chart_version | grep -P -o '^(\d+.){2}\d+')
  local new_chart_weekly=$(extract_k_version $latest_image_tag | grep -P -o '\d+')
  local new_chart_semver=$current_chart_semver
  if [[ $current_chart_version != *weekly* ]]; then
    # If previous version was not a weekly, then it was a stable release.
    # _This_ weekly release should have a semver that's one above the stable release.
    new_chart_semver=$(increment_semver $current_chart_semver 1)
    # Also reset the patch release number to 0.
    new_chart_semver=$(set_semver_patch_to_zero $new_chart_semver)
  fi
  echo "$new_chart_semver-weekly.$new_chart_weekly"
}

validate_version_update() {
  local new_chart_version=$1
  local current_chart_version=$2
  local latest_gel_tag=$3
  local latest_loki_tag=$4

  if [[ "$new_chart_version" == "$current_chart_version" ]]; then
    echo "New chart version ($new_chart_version) is the same as current version ($current_chart_version); not submitting weekly PR"
    exit 1
  fi

  local gel_weekly_version=$(extract_k_version $latest_gel_tag)
  local loki_weekly_version=$(extract_k_version $latest_loki_tag)
  echo "Comparing GEL weekly version ($gel_weekly_version) with Loki weekly version ($loki_weekly_version)"
  if [[ "$gel_weekly_version" != "$loki_weekly_version" ]]; then
    echo "GEL weekly version ($gel_weekly_version) does not match Loki weekly version ($loki_weekly_version); not submitting PR"
    exit 1
  fi
}

values_file=production/helm/loki/values.yaml
chart_file=production/helm/loki/Chart.yaml

latest_loki_tag=$(find_latest_image_tag grafana/loki)
latest_gel_tag=$(find_latest_image_tag grafana/enterprise-logs)
current_chart_version=$(get_yaml_node $chart_file .version)
new_chart_version=$(calculate_next_chart_version $current_chart_version $latest_loki_tag)

validate_version_update $new_chart_version $current_chart_version $latest_gel_tag $latest_loki_tag

update_yaml_node $values_file .loki.image.tag $latest_loki_tag
update_yaml_node $values_file .enterprise.image.tag $latest_gel_tag
update_yaml_node $chart_file .appVersion $(extract_k_version $latest_loki_tag)
update_yaml_node $chart_file .version $new_chart_version

sed --in-place \
  --regexp-extended \
  "s/(.*\<AUTOMATED_UPDATES_LOCATOR\>.*)/\1\n\n## ${new_chart_version}\n\n- \[CHANGE\] Changed version of Grafana Loki to ${latest_loki_tag}\n- \[CHANGE\] Changed version of Grafana Enterprise Logs to ${latest_gel_tag}/g" production/helm/loki/CHANGELOG.md

make TTY='' helm-docs

echo "::set-output name=new_chart_version::$new_chart_version"
