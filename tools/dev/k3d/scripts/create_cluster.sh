#!/usr/bin/env bash

current_dir="$(cd "$(dirname "$0")" && pwd)"
k3d_dir="$(cd "${current_dir}/.." && pwd)"

cluster_name="$1"
registry_port="$2"

namespace="k3d-${cluster_name}"
environment="${k3d_dir}/environments/${cluster_name}"
gex_plugins_checkout_dir=${GEX_PLUGINS_CHECKOUT_DIR:-"${current_dir}/../../../../../gex-plugins"}

echo "Creating ${cluster_name} kubernetes cluster"

k3d cluster create "${cluster_name}" \
	--servers 1 \
	--agents 3 \
	--volume "${gex_plugins_checkout_dir}/plugins/grafana-enterprise-logs-app:/var/lib/grafana/plugins/grafana-enterprise-logs-app" \
	--registry-use "k3d-grafana:${registry_port}" \
	--wait || true

cluster_node_list_json=$(k3d node list -o json)
cluster_port=$(echo "${cluster_node_list_json}" | jq -r ".[] | select(.name == \"k3d-${cluster_name}-serverlb\") | .portMappings.\"6443\"[] | .HostPort")
tk env set "${environment}" \
	--server="https://0.0.0.0:${cluster_port}" \
	--namespace="${namespace}"

kubectl config set-context "${namespace}"

namespaces=$(kubectl get namespaces)
if ! echo "${namespaces}" | grep -q -m 1 "${namespace}"; then
	kubectl create namespace "${namespace}" || true
fi

# Tanka does not apply CRDs from helm charts first, like helm does.
# This causes the helm deployments to fail due to missing CRDs.
# The following two sections pre-emptively apply the CRDs we need to the cluster.

# Apply CRDs needed for prometheus operator
prometheus_crd_base_url="https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/v0.67.1/example/prometheus-operator-crd"
for file in monitoring.coreos.com_alertmanagerconfigs.yaml \
	monitoring.coreos.com_alertmanagers.yaml \
	monitoring.coreos.com_podmonitors.yaml \
	monitoring.coreos.com_probes.yaml \
	monitoring.coreos.com_prometheuses.yaml \
	monitoring.coreos.com_prometheusrules.yaml \
	monitoring.coreos.com_servicemonitors.yaml \
	monitoring.coreos.com_thanosrulers.yaml; do
	kubectl apply \
		-f "${prometheus_crd_base_url}/${file}" \
		--force-conflicts=true \
		--server-side
done

# Apply CRDs needed for grafana agent
agent_crd_base_url="https://raw.githubusercontent.com/grafana/agent/7dbb39c70bbb67be40e528cb71a3541b59dbe93d/production/operator/crds"
for file in monitoring.grafana.com_grafanaagents.yaml \
	monitoring.grafana.com_integrations.yaml \
	monitoring.grafana.com_logsinstances.yaml \
	monitoring.grafana.com_metricsinstances.yaml \
	monitoring.grafana.com_podlogs.yaml; do
	kubectl apply \
		-f "${agent_crd_base_url}/${file}" \
		--force-conflicts=true \
		--server-side
done
