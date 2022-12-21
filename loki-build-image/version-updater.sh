#!/usr/bin/env bash

set -euo pipefail

VERSION="${1-}"
if [[ -z "${VERSION}" ]]; then
  >&2 echo "Usage: $0 <version>"
  exit 1
fi

echo "Updating loki-build-image references to '${VERSION}'"

find . -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*Dockerfile*' \) -exec grep -lP "grafana/loki-build-image:[0-9]+" {} \; | grep -ve '.drone' |
  while read -r x; do
    echo "Updating ${x}"
    sed -i -re "s,grafana/loki-build-image:[0-9]+\.[0-9]+\.[0-9]+,grafana/loki-build-image:${VERSION},g" "${x}"
  done