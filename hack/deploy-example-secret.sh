#!/bin/bash

set -eou pipefail

NAMESPACE=$1

REGION=""
ENDPOINT=""
ACCESS_KEY_ID=""
SECRET_ACCESS_KEY=""

set_credentials_from_aws() {
  AWS_CONFIG_FILE_NAME=$HOME/.aws/config
  AWS_CREDENTIALS_FILE_NAME=$HOME/.aws/credentials

  REGION="$(grep -m 1 region < $AWS_CONFIG_FILE_NAME | awk '{print $3}')"
  ENDPOINT="https://s3.${REGION}.amazonaws.com}"
  ACCESS_KEY_ID="$(grep -m 1 aws_access_key_id < $AWS_CREDENTIALS_FILE_NAME | awk '{print $3}')"
  SECRET_ACCESS_KEY="$(grep -m 1 aws_secret_access_key < $AWS_CREDENTIALS_FILE_NAME | awk '{print $3}')"
}

create_secret() {
  kubectl -n $NAMESPACE delete secret test ||:
  kubectl -n $NAMESPACE create secret generic test \
    --from-literal=endpoint=$(echo -n "$ENDPOINT" | base64) \
    --from-literal=region=$(echo -n "$REGION" | base64) \
    --from-literal=bucketnames=$(echo -n "loki" | base64) \
    --from-literal=access_key_id=$(echo -n "$ACCESS_KEY_ID" | base64) \
    --from-literal=access_key_secret=$(echo -n "$SECRET_ACCESS_KEY" | base64)
}

main() {
  set_credentials_from_aws
  create_secret
}

main
