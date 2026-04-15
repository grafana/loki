#!/usr/bin/env bash

set -euo pipefail

readonly account_name="${1-}"
readonly container_name="${2-}"

if [[ -z "${account_name}" ]]; then
    echo "Provide a account name"
    exit 1
fi

if [[ -z "${container_name}" ]]; then
    echo "Provide a container name"
    exit 1
fi

readonly namespace="${NAMESPACE:-openshift-logging}"

readonly azure_environment="AzureGlobal"

resource_group=$(az storage account show --name "${account_name}" | jq -r '.resourceGroup')
readonly resource_group

account_key=$(az storage account keys list --resource-group "${resource_group}" --account-name "${account_name}" | jq -r '.[0].value')
readonly account_key

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test \
    --from-literal=environment="$(echo -n "${azure_environment}")" \
    --from-literal=account_name="$(echo -n "${account_name}")" \
    --from-literal=account_key="$(echo -n "${account_key}")" \
    --from-literal=container="$(echo -n "${container_name}")"
