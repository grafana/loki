#!/usr/bin/env bash

set -euo pipefail

readonly bucket_name=${1-}
readonly google_application_credentials=${2-}

if [[ -z "${bucket_name}" ]]; then
    echo "Provide a bucket name"
    exit 1
fi

if [[ -z "${google_application_credentials}" ]]; then
    echo "Provide a path to the Google application credentials file"
    exit 1
fi

readonly namespace=${NAMESPACE:-openshift-logging}

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test \
    --from-literal=bucketname="$(echo -n "${bucket_name}")" \
    --from-file=key.json="${google_application_credentials}"
