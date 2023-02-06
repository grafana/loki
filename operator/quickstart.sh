#!/bin/bash

set -eou pipefail

# shellcheck disable=SC1091
source .bingo/variables.env

setup() {
    echo "-------------------------------------------"
    echo "- Creating Kind cluster...                -"
    echo "-------------------------------------------"
    # shellcheck disable=SC2154
    ${KIND} create cluster --config=hack/kind_config.yaml
}

deps() {
    echo "-------------------------------------------"
    echo "- Deploy Traefik Ingress Controller...    -"
    echo "-------------------------------------------"
    kubectl apply -f hack/addons_traefik.yaml
    kubectl -n traefik rollout status deployment traefik

    echo "-------------------------------------------"
    echo "- Deploy Hydra OIDC provider...           -"
    echo "-------------------------------------------"
    kubectl apply -f hack/addons_hydra.yaml
    kubectl -n hydra rollout status deployment hydra
    kubectl wait --timeout=180s -n hydra --for=condition=complete job/usercreator

    echo "-------------------------------------------"
    echo "- Deploy OIDC Token Refresher...          -"
    echo "-------------------------------------------"
    kubectl apply -f hack/addons_token_refresher.yaml
    kubectl rollout status deployment token-refresher
}

operator(){
    echo "-------------------------------------------"
    echo "- Deploy Loki Operator...                  -"
    echo "-------------------------------------------"
    make deploy
    kubectl rollout status deployment controller-manager
    kubectl rollout status deployment minio
}

lokistack(){
    echo "-------------------------------------------"
    echo "- Deploy Loki Stack...                    -"
    echo "-------------------------------------------"
    kubectl apply -f ./hack/lokistack_gateway_dev.yaml
}

logger() {
    echo "-------------------------------------------"
    echo "- Deploy Log Generator...                 -"
    echo "-------------------------------------------"
    kubectl apply -f ./hack/addons_logger.yaml
}

certificates() {
    echo "-------------------------------------------"
    echo "- Deploy TLS Certificates...              -"
    echo "-------------------------------------------"
    kubectl apply -f ./hack/addons_cert_manager.yaml
    kubectl -n cert-manager rollout status deployment cert-manager
    kubectl -n cert-manager rollout status deployment cert-manager-cainjector
    kubectl -n cert-manager rollout status deployment cert-manager-webhook
    kubectl apply -f ./hack/addons_kind_certs.yaml

    kubectl wait --timeout=180s --for=condition=ready certificate/lokistack-dev-signing-ca
    kubectl create configmap lokistack-dev-ca-bundle --from-literal service-ca.crt="$(kubectl get secret lokistack-dev-signing-ca -o json | jq -r '.data."ca.crt"' | base64 -d -)"
    kubectl create configmap lokistack-dev-gateway-ca-bundle --from-literal service-ca.crt="$(kubectl get secret lokistack-dev-signing-ca -o json | jq -r '.data."ca.crt"' | base64 -d -)"
}

check() {
    # shellcheck disable=SC2154
    ${LOGCLI} --addr "http://localhost/token-refresher/api/logs/v1/test-oidc" labels
}

case ${1:-"*"} in
setup)
    setup
    ;;

deps)
    deps
    ;;

operator)
    operator
    ;;

lokistack)
    lokistack
    ;;

logger)
    logger
    ;;

certificates)
    certificates
    ;;

check)
    check
    ;;

help)
    echo "usage: $(basename "$0") { setup | deps | operator | lokistack | logger | certificates | check }"
    ;;

*)
    setup
    deps
    operator
    certificates
    lokistack
    logger
    ;;
esac

wait
