#!/bin/bash

COMMUNITY_OPERATORS_REPOSITORY="k8s-operatorhub/community-operators"
UPSTREAM_REPOSITORY="redhat-openshift-ecosystem/community-operators-prod"
LOCAL_REPOSITORIES_PATH=${LOCAL_REPOSITORIES_PATH:-"$(dirname $(dirname $(dirname $(pwd))))"}

if [[ ! -d "${LOCAL_REPOSITORIES_PATH}/${COMMUNITY_OPERATORS_REPOSITORY}" ]]; then
    echo "${LOCAL_REPOSITORIES_PATH}/${COMMUNITY_OPERATORS_REPOSITORY} doesn't exist, aborting."
    exit 1
fi

if [[ ! -d "${LOCAL_REPOSITORIES_PATH}/${UPSTREAM_REPOSITORY}" ]]; then
    echo "${LOCAL_REPOSITORIES_PATH}/${UPSTREAM_REPOSITORY} doesn't exist, aborting."
    exit 1
fi

OLD_PWD=$(pwd)
VERSION=$(grep "COMMUNITY_RELEASE_VERSION=" Makefile | awk -F= '{print $2}')

for dest in ${COMMUNITY_OPERATORS_REPOSITORY} ${UPSTREAM_REPOSITORY}; do
    cd "${LOCAL_REPOSITORIES_PATH}/${dest}"
    git remote | grep upstream > /dev/null
    if [[ $? != 0 ]]; then
        echo "Cannot find a remote named 'upstream'. Adding one."
        git remote add upstream git@github.com:${dest}.git
    fi

    git fetch -q upstream
    git checkout -q main
    git rebase -q upstream/main

    mkdir -p "operators/loki-operator/${VERSION}"
    cp -r ${OLD_PWD}/bundle/community/* "operators/loki-operator/${VERSION}/"
    rm "operators/loki-operator/${VERSION}/bundle.Dockerfile"

    if [[ "$dest" = "${UPSTREAM_REPOSITORY}" ]]; then
        python3 - << END
import os, yaml
with open("./operators/loki-operator/${VERSION}/metadata/annotations.yaml", 'r') as f:
    y=yaml.safe_load(f) or {}
    y['annotations']['com.redhat.openshift.versions'] = os.getenv('SUPPORTED_OCP_VERSIONS')
with open("./operators/loki-operator/${VERSION}/metadata/annotations.yaml", 'w') as f:
    yaml.dump(y, f)
END
    fi

    git checkout -q -b update-loki-operator-to-${VERSION}
    if [[ $? != 0 ]]; then
        echo "Cannot switch to the new branch update-loki-operator-${dest}-to-${VERSION}. Aborting"
        exit 1
    fi

    git add .
    git commit -sqm "Update loki-operator to v${VERSION}"

    command -v gh > /dev/null
    if [[ $? != 0 ]]; then
        echo "'gh' command not found, can't submit the PR on your behalf."
        break
    fi

    echo "Submitting PR on your behalf via 'hub'"
    # gh pr create --title  "Update loki-operator to v${VERSION}" --body-file "${OLD_PWD}/hack/.checked-pr-template.md"
done

cd ${OLD_PWD}
echo "Completed."
