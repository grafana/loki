<p align="center"><img src="../docs/sources/logo.png" alt="Loki Logo"></p>

# Loki Operator

This is the Kubernetes Operator for [Loki](https://grafana.com/docs/loki/latest/)
provided by the Grafana Loki SIG operator. **This is currently a work in
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
check our [documentation](https://github.com/grafana/loki/blob/master/operator/docs/hack_loki_operator.md)

Also, there is a [document](https://github.com/grafana/loki/blob/master/operator/docs/hack_operator_make_run.md) which
demonstrates how to use Loki Operator for development and testing locally without deploying the operator each time on Kind and OpenShift.
