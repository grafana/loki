[![go test](https://github.com/ViaQ/loki-operator/workflows/go%20test/badge.svg)](https://github.com/ViaQ/loki-operator/actions)
[![Coveralls github](https://img.shields.io/coveralls/github/ViaQ/loki-operator.svg)](https://coveralls.io/github/ViaQ/loki-operator)
[![Report Card](https://goreportcard.com/badge/github.com/ViaQ/loki-operator)](https://goreportcard.com/report/github.com/ViaQ/loki-operator)

![](img/loki-operator.png)

# Loki Operator by Red Hat

This is the Kubernetes Operator for [Loki](https://grafana.com/docs/loki/latest/)
provided by the Red Hat OpenShift engineering team. **This is currently a work in
progress and is subject to large scale changes that will break any dependencies.
Do not use this in any production environment.**

## Development

Requirements:

  1. Running Kubernetes cluster. Our team uses
     [KinD](https://kind.sigs.k8s.io/docs/user/quick-start/) or
     [K3s](https://k3s.io/) for simplicity.
  1. A container registry that you and your Kubernetes cluster can reach. We
     recommend [quay.io](https://quay.io/signin/).

Build and push the container image and then deploy the operator with `make
oci-build oci-push deploy IMG=quay.io/my-team/loki-operator:latest`.  This will
deploy to your active Kubernetes/OpenShift cluster defined by your local
[kubeconfig](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/).

For detailed step-by-step guide on how to start development and testing on Kind and OpenShift, 
check our [documentation](https://github.com/ViaQ/loki-operator/blob/master/docs/hack_loki_operator.md)
