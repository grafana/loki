#!/usr/bin/env bash

current_dir="$(cd "$(dirname "${0}")" && pwd)"
loki_dir="$(cd "${current_dir}/../cmd/loki" && pwd)"

export OLD_LOKI=${OLD_VERSION:-2.7.5}
export NEW_LOKI=${NEW_VERSION:-$("${current_dir}/image-tag")}

export CONFIG_FILE="loki-local-config.yaml"

function get_config() {
	local version=${1}

	docker run --rm -t -v "${loki_dir}:/config" "grafana/loki:${version}" \
		-config.file="/config/${CONFIG_FILE}" \
		-print-config-stderr 2>&1
}

function parse_config() {
	sed '/Starting Loki/q' | tr -d '\r'
}

tmp_dir="$(mktemp -d)"
old_config="${tmp_dir}/config-${OLD_LOKI}.yml"
new_config="${tmp_dir}/config-${NEW_LOKI}.yml"

echo "Saving configs to ${tmp_dir}"
echo "Old config: ${old_config}"
echo "New config: ${new_config}"

get_config "${OLD_LOKI}" | parse_config > "${old_config}"
get_config "${NEW_LOKI}" | parse_config > "${new_config}"

diff --color=always \
  --side-by-side \
  "${old_config}" \
  "${new_config}"
