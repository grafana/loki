#!/usr/bin/env bash

set -euo pipefail

# The BSD version of the sed command in MacOS doesn't work with this script.
# Please install gnu-sed via `brew install gnu-sed`.
# The gsed command becomes available then.
SED="sed"
if command -v gsed &> /dev/null ; then
  echo "Using gsed"
  SED="gsed"
fi

VERSION="${1-}"
if [[ -z "${VERSION}" ]]; then
  >&2 echo "Usage: $0 <version>"
  exit 1
fi

echo "Updating loki-build-image references to '${VERSION}'"

find . -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*Dockerfile*' -o -name '*devcontainer.json' \) -exec grep -lE "grafana/loki-build-image:[0-9]+" {} \; | grep -ve '.drone' |
  while read -r x; do
    echo "Updating ${x}"
    ${SED} -i -re "s,grafana/loki-build-image:[0-9]+\.[0-9]+\.[0-9]+,grafana/loki-build-image:${VERSION},g" "${x}"
  done
