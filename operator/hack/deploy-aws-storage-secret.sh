#!/usr/bin/env bash

set -euo pipefail

readonly bucket_name=${1-}

if [[ -z "${bucket_name}" ]]; then
    echo "Provide a bucket name"
    exit 1
fi

readonly namespace=${NAMESPACE:-openshift-logging}
region=${REGION:-$(aws configure get region)}
readonly region
access_key_id=${ACCESS_KEY_ID:-$(aws configure get aws_access_key_id)}
readonly access_key_id
secret_access_key=${SECRET_ACCESS_KEY:-$(aws configure get aws_secret_access_key)}
readonly secret_access_key

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test \
  --from-literal=region="$(echo -n "${region}")" \
  --from-literal=bucketnames="$(echo -n "${bucket_name}")" \
  --from-literal=access_key_id="$(echo -n "${access_key_id}")" \
  --from-literal=access_key_secret="$(echo -n "${secret_access_key}")" \
  --from-literal=endpoint="$(echo -n "https://s3.${region}.amazonaws.com")"
