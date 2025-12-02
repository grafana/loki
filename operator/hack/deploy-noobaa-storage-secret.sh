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

readonly bucket_claim_name=${1-}

if [[ -z "${bucket_claim_name}" ]]; then
    echo "Provide a bucket name"
    exit 1
fi

readonly namespace=${NAMESPACE:-openshift-logging}
readonly region

# static authentication from the current select AWS CLI profile.
bucket_name=${BUCKET_NAME:-$(oc -n $namespace get configmap $bucket_claim_name -o json | jq -r '.data.BUCKET_NAME')}
readonly bucket_name
access_key_id=${ACCESS_KEY_ID:-$(oc -n $namespace get secret $bucket_claim_name -o json | jq -r '.data.AWS_ACCESS_KEY_ID' | base64 -d)}
readonly access_key_id
secret_access_key=${SECRET_ACCESS_KEY:-$( oc -n $namespace get secret $bucket_claim_name -o json | jq -r '.data.AWS_SECRET_ACCESS_KEY' | base64 -d)}
readonly secret_access_key


create_secret_args=( \
  --from-literal=bucketnames="$(echo -n "${bucket_name}")" \
  --from-literal=access_key_id="$(echo -n "${access_key_id}")" \
  --from-literal=access_key_secret="$(echo -n "${secret_access_key}")" \
  --from-literal=endpoint="$(echo -n "https://s3.openshift-storage.svc")" \
)

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test "${create_secret_args[@]}"

cat << 'EOF'

Remember to update your LokiStack CR with:
spec:
  storage:
    tls:
      caName: openshift-service-ca.crt

EOF