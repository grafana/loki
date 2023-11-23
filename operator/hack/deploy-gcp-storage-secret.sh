#!/bin/bash

set -eo pipefail

if [ ! "$1" ]; then
    echo "Provide a bucket name"
    exit 1
fi

if [ ! "$2" ]; then
    echo "Provide a path to the Google application credentials file"
    exit 1
fi

BUCKET_NAME=$1
GOOGLE_APPLICATION_CREDENTIALS=$2

NAMESPACE=${NAMESPACE:-openshift-logging}

kubectl --ignore-not-found=true -n "${NAMESPACE}" delete secret test
kubectl -n "${NAMESPACE}" create secret generic test \
  --from-literal=bucketname="$(echo -n "${BUCKET_NAME}")" \
  --from-file=key.json="${GOOGLE_APPLICATION_CREDENTIALS}"
