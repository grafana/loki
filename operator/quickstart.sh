#!/bin/bash

set -eou pipefail

setup() {
    echo "-------------------------------------------"
    echo "- Creating Kind cluster...                -"
    echo "-------------------------------------------"
    kind create cluster --config=hack/kind_config.yaml
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
    make oci-build oci-push deploy REGISTRY_ORG=periklis
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

check() {
    logcli --addr "http://localhost/token-refresher/api/logs/v1/test-oidc" labels
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

check)
    check
    ;;

help)
    echo "usage: $(basename "$0") { setup | deps | operator | lokistack | logger | check }"
    ;;

*)
    setup
    deps
    operator
    lokistack
    logger
    ;;
esac

wait
