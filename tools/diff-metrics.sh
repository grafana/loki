#!/usr/bin/env bash
# To use this script
# * run the script, it will spin up 2 loki instances and print the local port 3100 is bound to
# * in another terminal, curl the two instances /metrics endpoints, and save to a file
# * diff the files
# * press enter to kill the servers

set -eo pipefail

current_dir="$(cd "$(dirname "${0}")" && pwd)"
loki_dir="$(cd "${current_dir}/../cmd/loki" && pwd)"
root_dir="$(cd "${current_dir}/.." && pwd)"

export OLD_LOKI=${OLD_VERSION:-2.7.5}
export NEW_LOKI=${NEW_VERSION:-$("${current_dir}/image-tag")}

export CONFIG_FILE="loki-local-config.yaml"

function start_loki() {
	local version=${1}

	docker run --rm -t -d -v "${loki_dir}:/config" \
    -p 3100 \
    "grafana/loki:${version}" \
		-config.file="/config/${CONFIG_FILE}"
}


make -C "${root_dir}" loki-image

loki1="$(start_loki "${OLD_LOKI}")"
loki2="$(start_loki "${NEW_LOKI}")"

echo "Loki 1: ${loki1}"
echo "Loki 2: ${loki2}"

docker port "${loki1}"
docker port "${loki2}"

echo "Curl instances on ports above to get metrics."
read -r -n 1 -p "Press enter to kill loki instances..."

docker kill "${loki1}" "${loki2}"

