#!/bin/bash

current_dir="$(cd "$(dirname "$0")" && pwd)"
k3d_dir="$(cd "${current_dir}/.." && pwd)"

cluster_name="$1"
namespace="k3d-${cluster_name}"
environment="${k3d_dir}/environments/${cluster_name}"
registry_port="${REGISTRY_PORT:-45629}"

if k3d registry list | grep -q -m 1 k3d-grafana; then
	k3d registry create k3d-grafana \
		--port "${registry_port}"
fi

if k3d cluster list | grep -q -m 1 "${cluster_name}"; then
	k3d cluster create "${cluster_name}" \
		--servers 1 \
		--agents 3 \
		--registry-use "k3d-grafana:${registry_port}" \
		--wait
fi

tk env set "${environment}" \
	--server="https://0.0.0.0:$(k3d node list -o json | jq -r ".[] | select(.name == \"k3d-${cluster_name}-serverlb\") | .portMappings.\"6443\"[] | .HostPort")" \
	--namespace="${namespace}"

kubectl config set-context "${namespace}"

if kubectl get namespaces | grep -q -m 1 "${namespace}"; then
	kubectl create namespace "${namespace}" || true
fi

tk apply "${environment}"
