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

# The loki-build-image Dockerfile and the dev container take the Go version via a
# GO_VERSION build arg (sourced from the Makefile at build time) rather than a
# literal "FROM golang:<ver>", so update the pinned arg in the dev container too.
DEVCONTAINER=".devcontainer/devcontainer.json"
if [[ -f "${DEVCONTAINER}" ]]; then
  echo "Updating ${DEVCONTAINER}"
  ${SED} -i -re "s,(\"GO_VERSION\": \")[0-9\.]+(\"),\1${VERSION}\2,g" "${DEVCONTAINER}"
fi