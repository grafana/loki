#!/bin/bash

set -eou pipefail

BUCKET_NAME=$1

NAMESPACE=${NAMESPACE:-openshift-logging}

REGION=${REGION:-$(aws configure get region)}
ACCESS_KEY_ID=${ACCESS_KEY_ID:-$(aws configure get aws_access_key_id)}
SECRET_ACCESS_KEY=${SECRET_ACCESS_KEY:-$(aws configure get aws_secret_access_key)}

kubectl --ignore-not-found=true -n "${NAMESPACE}" delete secret test
kubectl -n "${NAMESPACE}" create secret generic test \
  --from-literal=region="$(echo -n "${REGION}")" \
  --from-literal=bucketnames="$(echo -n "${BUCKET_NAME}")" \
  --from-literal=access_key_id="$(echo -n "${ACCESS_KEY_ID}")" \
  --from-literal=access_key_secret="$(echo -n "${SECRET_ACCESS_KEY}")" \
  --from-literal=endpoint="$(echo -n "https://s3.${REGION}.amazonaws.com")"
