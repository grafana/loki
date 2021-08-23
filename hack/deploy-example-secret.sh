#!/bin/bash

set -eou pipefail

NAMESPACE=$1

REGION=""
ENDPOINT=""
ACCESS_KEY_ID=""
SECRET_ACCESS_KEY=""
LOKI_BUCKET_NAME="${LOKI_BUCKET_NAME:-loki}"

set_credentials_from_aws() {
  REGION="$(aws configure get region)"
  ACCESS_KEY_ID="$(aws configure get aws_access_key_id)"
  SECRET_ACCESS_KEY="$(aws configure get aws_secret_access_key)"
  ENDPOINT="https://s3.${REGION}.amazonaws.com"
}

create_secret() {
  kubectl -n $NAMESPACE delete secret test ||:
  kubectl -n $NAMESPACE create secret generic test \
    --from-literal=endpoint=$(echo -n "$ENDPOINT") \
    --from-literal=region=$(echo -n "$REGION") \
    --from-literal=bucketnames=$(echo -n "$LOKI_BUCKET_NAME") \
    --from-literal=access_key_id=$(echo -n "$ACCESS_KEY_ID") \
    --from-literal=access_key_secret=$(echo -n "$SECRET_ACCESS_KEY")
}

main() {
  set_credentials_from_aws
  create_secret
}

main
