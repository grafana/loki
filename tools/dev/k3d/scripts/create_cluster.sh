#!/bin/bash

current_dir="$(cd "$(dirname "$0")" && pwd)"
k3d_dir="$(cd "${current_dir}/.." && pwd)"

cluster_name="$1"
namespace="k3d-${cluster_name}"
environment="${k3d_dir}/environments/${cluster_name}"

tk env set "${environment}" \
	--server="https://0.0.0.0:$(k3d node list -o json | jq -r ".[] | select(.name == \"k3d-${cluster_name}-serverlb\") | .portMappings.\"6443\"[] | .HostPort")" \
	--namespace="${namespace}"

kubectl config set-context "${namespace}"

if ! kubectl get namespaces | grep -q -m 1 "${namespace}"; then
	kubectl create namespace "${namespace}" || true
fi

tk apply "${environment}"
