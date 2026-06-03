#!/usr/bin/env bash

set -euo pipefail

function print_green() {
  echo -e "\033[32m$1\033[0m";
}

function print_red() {
  echo -e "\033[31m$1\033[0m";
}

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
  >&2 print_red "Usage: $0 <version>"
  exit 1
fi

EXCLUDE_DIRS="-name operator -prune -o -name vendor -prune -o"

print_green "Updating golang base images to '${VERSION}'"
find $EXCLUDE_DIRS -type f -name "Dockerfile*" -exec grep -lE "FROM golang:" {} \; |
  while read -r x; do
    echo " Checking ${x}"
    ${SED} -i -re "s,golang:[0-9\.]+,golang:${VERSION},g" "${x}"
  done

print_green "Updating GO_VERSION env variable in workflows to '${VERSION}'"
find .github/workflows/ -type f -name "*.yml" -exec grep -lE "GO_VERSION:" {} \; |
  while read -r x; do
    echo " Checking ${x}"
    ${SED} -i -re "s,GO_VERSION: \"[0-9\.]+\",GO_VERSION: \"${VERSION}\",g" "${x}"
  done

print_green "Updating go-version in workflows to '${VERSION}'"
find .github/workflows/ -type f -name "*.yml" -exec grep -lE "go-version:" {} \; |
  while read -r x; do
    echo " Checking ${x}"
    ${SED} -i -re "s,go-version: \"[0-9\.]+\",go-version: \"${VERSION}\",g" "${x}"
  done
