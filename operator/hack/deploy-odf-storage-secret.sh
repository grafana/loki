#!/usr/bin/env bash
#
# usage: deploy-odf-storage-secret.sh <bucket-claim-name>
#
# This scripts deploys a LokiStack Secret resource holding the
# authentication credentials to access ODF S3 bucket provided by the bucket
# claim name.
#
# bucket_claim_name is the name of the bucket claim to be used by this script to
# fetch the credentials to access the bucket.


set -euo pipefail

readonly bucket_claim_name=${1-}

if [[ -z "${bucket_claim_name}" ]]; then
    echo "Provide a bucket claim name"
    exit 1
fi

readonly namespace=${NAMESPACE:-openshift-logging}

# static authentication from the current select AWS CLI profile.
bucket_name=${BUCKET_NAME:-$(oc -n "${namespace}" get configmap "${bucket_claim_name}" -o json | jq -r '.data.BUCKET_NAME')}
readonly bucket_name
access_key_id=${ACCESS_KEY_ID:-$(oc -n "${namespace}" get secret "${bucket_claim_name}" -o json | jq -r '.data.AWS_ACCESS_KEY_ID' | base64 -d)}
readonly access_key_id
secret_access_key=${SECRET_ACCESS_KEY:-$( oc -n "${namespace}" get secret "${bucket_claim_name}" -o json | jq -r '.data.AWS_SECRET_ACCESS_KEY' | base64 -d)}
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
