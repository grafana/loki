#!/bin/bash

set -euo pipefail

# sed-wrap runs the appropriate sed command based on the
# underlying value of $OSTYPE on all non-binary files.
sed-wrap() {
    # TODO(kavi): Should I accept the arg?
  if [[ "${OSTYPE}" == "linux"* ]]; then
    # Linux
    find . -type f -not -path "./.git/*" -not -path "./vendor/*" -not -path "./operator/*" -exec grep -Iq . {} \; -print0 | xargs -0 sed -i -E "$1"
  else
    # macOS, BSD
    find . -type f -not -path "./.git/*" -not -path "./vendor/*" -not -path "./operator/*" -exec grep -Iq . {} \; -print0 | xargs -0 sed -i '' -E "$1"
  fi
}

OLD_VERSION=${LOKI_OLD_VERSION}
NEW_VERSION=${LOKI_NEW_VERSION}

if [ -z "$OLD_VERSION" ]
then
   read -rp "Enter old release version (eg 2.9.1): " OLD_VERSION
fi

if [ -z "$NEW_VERSION" ]
then
    read -rp "Enter new release version (eg 2.9.2): " NEW_VERSION
fi

LOKI_DOCKER_DRIVER_TAG="grafana\/loki-docker-driver:"
LOKI_DOCUMENTS_TAG="loki\/v"
LOKI_DOCKER_TAG="grafana/loki:"
LOKI_PROMTAIL_DOCKER_TAG="grafana/promtail:"
LOKI_CANARY_DOCKER_TAG="grafana/loki-canary:"
LOKI_LOGCLI_DOCKER_TAG="grafana/logcli:"

echo "Updating image tags"

# loki docker driver
sed-wrap "s/($LOKI_DOCKER_DRIVER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g;s/($LOKI_DOCUMENTS_TAG)($OLD_VERSION)/\1$NEW_VERSION/g"
