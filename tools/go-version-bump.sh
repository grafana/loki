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

echo "Updating golang base images to '${VERSION}'"

find cmd clients tools loki-build-image -type f -name 'Dockerfile*' -exec grep -lE "FROM golang:[0-9\.]+" {} \; |
  while read -r x; do
    echo "Updating ${x}"
    ${SED} -i -re "s,golang:[0-9\.]+,golang:${VERSION},g" "${x}"
  done