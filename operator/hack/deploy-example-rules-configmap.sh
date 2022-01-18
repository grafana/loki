#!/bin/bash

set -eou pipefail

NAMESPACE=$1

create_rules_configmap() {
  kubectl -n "$NAMESPACE" delete configmap rules ||:
  kubectl -n "$NAMESPACE" create configmap rules \
    --from-file=rules.yaml=hack/ruler_rules.yaml
}

main() {
  create_rules_configmap
}

main