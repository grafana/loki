#!/usr/bin/env bash
#
# usage: deploy-azure-storage-secret.sh <account_name> <container_name> (<client_id> <tenant_id> <subscription_id>)
#
# This scripts deploys a LokiStack Secret resource holding the
# authentication credentials to access Azure Blob Storage. It supports three
# modes: static authentication, managed with custom managed identity, and
# fully managed by OpenShift's Cloud-Credentials-Operator. To use
# one of the managed modes you need to pass the environment variable
# STS=true. If you pass the three optional arguments you can set
# your custom managed identity credentials.
#
# account_name is the name of the Azure storage account to be used.
#
# container_name is the name of the container to be used in the LokiStack
# object storage secret.
#
# client_id is the UUID of the Managed Identity accessing the storage.
#
# tenant_id is the UUID of the Tenant hosting the Managed Identity.
#
# subscription_id is the UUID of the subscription hosting the Managed Identity.
#

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

# workload identity authentication with/without a manually provisioned Managed Identity.
readonly sts="${STS:-false}"
readonly client_id="${3-}"
readonly tenant_id="${4-}"
readonly subscription_id="${5-}"

create_secret_args=( \
  --from-literal=environment="$(echo -n "${azure_environment}")" \
  --from-literal=account_name="$(echo -n "${account_name}")" \
  --from-literal=container="$(echo -n "${container_name}")" \
)

if [[ "${sts}" = "true" ]]; then
    if [[ -n "${client_id}" ]] && [[ -n "${tenant_id}" ]] && [[ -n "${subscription_id}" ]]; then
        # Managed with custom managed identity
        create_secret_args+=( \
            --from-literal=client_id="$(echo -n "${client_id}")" \
            --from-literal=tenant_id="$(echo -n "${tenant_id}")" \
            --from-literal=subscription_id="$(echo -n "${subscription_id}")" \
        )
    fi
    # If client_id/tenant_id/subscription_id are not provided, this is fully managed by CCO
    # and we only include the basic storage configuration (no credentials)
else
    # static authentication from Azure CLI
    resource_group=$(az storage account show --name "${account_name}" | jq -r '.resourceGroup')
    readonly resource_group

    account_key=$(az storage account keys list --resource-group "${resource_group}" --account-name "${account_name}" | jq -r '.[0].value')
    readonly account_key

    create_secret_args+=(--from-literal=account_key="$(echo -n "${account_key}")")
fi

kubectl --ignore-not-found=true -n "${namespace}" delete secret test
kubectl -n "${namespace}" create secret generic test "${create_secret_args[@]}"
