#!/bin/bash

set -eou pipefail

USER_WORKLOAD_NAMESPACE=openshift-user-workload-monitoring
NAMESPACE=openshift-logging

secret=$(kubectl -n "${USER_WORKLOAD_NAMESPACE}" get secret | grep prometheus-user-workload-token | head -n 1 | awk '{print $1 }')
PROMETHEUS_URL="https://$(kubectl -n openshift-monitoring get route thanos-querier -o json | jq -r '.spec.host')"
PROMETHEUS_TOKEN=$(kubectl -n "${USER_WORKLOAD_NAMESPACE}" get secret "${secret}" -o json | jq -r '.data.token' | base64 -d)

kubectl -n "${NAMESPACE}" delete secret promsecret ||:
kubectl -n "${NAMESPACE}" create secret generic promsecret \
    --from-literal=prometheus_url="$(echo -n "${PROMETHEUS_URL}")" \
    --from-literal=prometheus_token="$(echo -n "${PROMETHEUS_TOKEN}")"
