#!/bin/bash

set -euo pipefail

OLD_VERSION=${LOKI_OLD_VERSION:-}
NEW_VERSION=${LOKI_NEW_VERSION:-}

if [ -z "$OLD_VERSION" ]
then
    OLD_VERSION="[0-9]+\.[0-9]+\.[0-9]+"
fi

if [ -z "$NEW_VERSION" ]
then
    read -rp "Enter new release version (eg 2.9.2): " NEW_VERSION
fi


LOKI_DOCKER_DRIVER_TAG="grafana\/loki-docker-driver:"
LOKI_DOCUMENTS_TAG="loki\/v"
LOKI_DOCKER_TAG="grafana\/loki:"
LOKI_PROMTAIL_DOCKER_TAG="grafana\/promtail:"
LOKI_CANARY_DOCKER_TAG="grafana\/loki-canary:"
LOKI_LOGCLI_DOCKER_TAG="grafana\/logcli:"

echo "Updating version references to $NEW_VERSION"

# grep -Iq is to ignore non-binary files.
find . -type f -not -path "./.git/*" -not -path "./vendor/*" -not -path "./operator/*" -not -path "./CHANGELOG.md" -not -path "./docs/sources/setup/upgrade/*" -exec grep -Iq . {} \; -print0\
    | xargs -0 sed -i '' -E \
	    -e "s/($LOKI_DOCKER_DRIVER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
	    -e "s/($LOKI_DOCUMENTS_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
	    -e "s/($LOKI_DOCKER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
	    -e "s/($LOKI_PROMTAIL_DOCKER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
	    -e "s/($LOKI_CANARY_DOCKER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
	    -e "s/($LOKI_LOGCLI_DOCKER_TAG)($OLD_VERSION)/\1$NEW_VERSION/g" \
