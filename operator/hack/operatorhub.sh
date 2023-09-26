#!/usr/bin/env bash

set -e -u -o pipefail

COMMUNITY_OPERATORS_REPOSITORY="k8s-operatorhub/community-operators"
UPSTREAM_REPOSITORY="redhat-openshift-ecosystem/community-operators-prod"
LOCAL_REPOSITORIES_PATH=${LOCAL_REPOSITORIES_PATH:-"$(dirname "$(dirname "$(dirname "$(pwd)")")")"}

if [[ ! -d "${LOCAL_REPOSITORIES_PATH}/${COMMUNITY_OPERATORS_REPOSITORY}" ]]; then
    echo "${LOCAL_REPOSITORIES_PATH}/${COMMUNITY_OPERATORS_REPOSITORY} doesn't exist, aborting."
    exit 1
fi

if [[ ! -d "${LOCAL_REPOSITORIES_PATH}/${UPSTREAM_REPOSITORY}" ]]; then
    echo "${LOCAL_REPOSITORIES_PATH}/${UPSTREAM_REPOSITORY} doesn't exist, aborting."
    exit 1
fi

SOURCE_DIR=$(pwd)
VERSION=$(grep "VERSION ?= " Makefile | awk -F= '{print $2}' | xargs)

for dest in ${COMMUNITY_OPERATORS_REPOSITORY} ${UPSTREAM_REPOSITORY}; do
    (
        cd "${LOCAL_REPOSITORIES_PATH}/${dest}" || exit

        if ! git remote | grep upstream > /dev/null;
        then
            echo "Cannot find a remote named 'upstream'. Adding one."
            git remote add upstream "git@github.com:${dest}.git"
        fi

        git fetch -q upstream
        git checkout -q main
        git rebase -q upstream/main

        mkdir -p "operators/loki-operator/${VERSION}"
        if [[ "${dest}" = "${UPSTREAM_REPOSITORY}" ]]; then
            cp -r "${SOURCE_DIR}/bundle/community-openshift"/* "operators/loki-operator/${VERSION}/"
        else
            cp -r "${SOURCE_DIR}/bundle/community"/* "operators/loki-operator/${VERSION}/"
        fi
        rm "operators/loki-operator/${VERSION}/bundle.Dockerfile"

        if [[ "${dest}" = "${UPSTREAM_REPOSITORY}" ]]; then
            python3 - << END
import os, yaml
with open("./operators/loki-operator/${VERSION}/metadata/annotations.yaml", 'r') as f:
    y=yaml.safe_load(f) or {}
    y['annotations']['com.redhat.openshift.versions'] = os.getenv('SUPPORTED_OCP_VERSIONS')
with open("./operators/loki-operator/${VERSION}/metadata/annotations.yaml", 'w') as f:
    yaml.dump(y, f)
END
        fi

        if ! git checkout -q -b "update-loki-operator-to-${VERSION}";
        then
            echo "Cannot switch to the new branch update-loki-operator-${dest}-to-${VERSION}. Aborting"
            exit 1
        fi

        git add .
        git commit -sqm "Update loki-operator to ${VERSION}"

        if ! command -v gh > /dev/null;
        then
            echo "'gh' command not found, can't submit the PR on your behalf."
            exit 0
        fi

        echo "Submitting PR on your behalf via 'gh'"
        gh pr create --title  "Update loki-operator to ${VERSION}" --body-file "${SOURCE_DIR}/hack/.checked-pr-template.md"
    )
done

echo "Completed."
