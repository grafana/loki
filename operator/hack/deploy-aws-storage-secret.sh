#!/usr/bin/env bash
#
# usage: deploy-aws-storage-secret.sh <bucket-name> (<role_arn>)
#
# This scripts deploys a LokiStack Secret resource holding the
# authentication credentials to access AWS S3. It supports three
# modes: static authentication, managed with custom role_arn and
# fully managed by OpeShift's Cloud-Credentials-Operator. To use
# one of the managed you need to pass the environment variable
# STS=true. If you pass the second optional argument you can set
# your custom managed role_arn.
#
# bucket_name is the name of the bucket to be used in the LokiStack
# object storage secret.
#
# role_arn is the ARN value of the upfront manually provisioned AWS
# Role that grants access to the <bucket_name> and it's object on
# AWS S3.
#

set -euo pipefail

readonly bucket_name=${1-}

if [[ -z "${bucket_name}" ]]; then
    echo "Provide a bucket name"
    exit 1
fi

readonly namespace=${NAMESPACE:-openshift-logging}
region=${REGION:-$(aws configure get region)}
readonly region

# static authentication from the current select AWS CLI profile.
access_key_id=${ACCESS_KEY_ID:-$(aws configure get aws_access_key_id)}
readonly access_key_id
secret_access_key=${SECRET_ACCESS_KEY:-$(aws configure get aws_secret_access_key)}
readonly secret_access_key

# token authentication with/without a manually provisioned AWS Role.
readonly sts=${STS:-false}
readonly role_arn=${2-}

create_secret_args=( \
  --from-literal=region="$(echo -n "${region}")" \
  --from-literal=bucketnames="$(echo -n "${bucket_name}")" \
)

if [[ "${sts}" = "true" ]]; then
    if [[ -n "${role_arn}" ]]; then
        create_secret_args+=(--from-literal=role_arn="$(echo -n "${role_arn}")")
    fi
else
    create_secret_args+=( \
        --from-literal=access_key_id="$(echo -n "${access_key_id}")" \
        --from-literal=access_key_secret="$(echo -n "${secret_access_key}")" \
        --from-literal=endpoint="$(echo -n "https://s3.${region}.amazonaws.com")" \
    )
fi

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test "${create_secret_args[@]}"
