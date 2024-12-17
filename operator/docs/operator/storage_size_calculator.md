# Install Storage Size Calculator on OpenShift

This document demonstrates how to install storage size calculator for loki on OpenShift.

_Note:_ The storage size calculator works out of the box on OpenShift.

## Introduction

Storage Size Calculator is used to have an idea on how to properly size a Loki cluster. It spins up a log collector that is used to collect metrics for a period of time and based on the amount of logs being collected, extrapolate the amount of logs required for a day and from that recommend a t-shirt size.

## Requirements

* Create a running OpenShift cluster.
* A container registry that you and your OpenShift cluster can reach. We recommend [quay.io](https://quay.io/signin/).

## Installation

* Deploy the [Loki Operator](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#hacking-on-loki-operator-on-openshift) to the cluster.

* Create the `openshift-logging` namespace in the cluster:

  ```console
  kubectl create ns openshift-logging
  ```

* Deploy the storage size calculator by executing following command in the terminal:

  ```console
  make deploy-size-calculator REGISTRY_ORG_OPENSHIFT=$YOUR_QUAY_ORG
  ```

  where `$YOUR_QUAY_ORG` is your personal [quay.io](https://quay.io) account where you can push container images.

  You should see `log-file-metric-exporter-xxx` and `storage-size-calculator-xxx` pods running.

  _Note:_ [log-file-metric-exporter](https://github.com/ViaQ/log-file-metric-exporter) is used to collect metrics about container logs being produced in a kubernetes environment. It publishes `log_logged_bytes_total` metric to prometheus.

* Now you can check the logs to see the recommended t-shirt size for your cluster:

  ```console
  kubectl -n openshift-logging logs <POD_NAME>
  ```

  where `<POD_NAME>` is the name of the storage size calculator pod and can be found using:

  ```console
  kubectl -n openshift-logging get pods
  ```

  _Note:_ The storage size calculator logs the recommended t-shirt size every minute.

## Cleanup

To cleanup the deployment you can use:

```console
make undeploy-size-calculator
```

This will cleanup the resources related to storage size calculator. However, the Loki Operator would still be running.

## Contribution

If you want to contribute to the storage size calculator, you can follow this local development and testing process.

* Fork and clone the [Loki Operator](https://github.com/grafana/loki/tree/main/operator) repo.

* All the files related to storage size calculator exists at [`operator/config/overlays/openshift/size-calculator`](https://github.com/grafana/loki/tree/main/operator/config/overlays/openshift/size-calculator) and the main file is at [`operator/cmd/size-calculator`](https://github.com/grafana/loki/tree/main/operator/cmd/size-calculator).

* Update the code to fix a bug or add a new feature.

* To test the changes made, build the image and push it to quay. Replace the `image` value in [storage_size_calculator.yaml](https://github.com/grafana/loki/blob/main/operator/config/overlays/openshift/size-calculator/storage_size_calculator.yaml) with your quay image to test the changes.

  Build the image using:

  ```console
  make oci-build-calculator
  ```

  This will build the storage size calculator image using [dockerfile](https://github.com/grafana/loki/blob/main/operator/calculator.Dockerfile)

  Push the image to quay using:

  ```console
  make oci-push-calculator
  ```

  After replacing the image name, deploy the storage size calculator to test your changes:

  ```console
  make deploy-size-calculator REGISTRY_ORG_OPENSHIFT=$YOUR_QUAY_ORG
  ```

  where `$YOUR_QUAY_ORG` is your personal [quay.io](https://quay.io) account where you pushed your container image.

* If everything works fine then create a pull request.

## Troubleshooting

### Permission denied on deploying prometheus secret

If you get `permission denied` error while running `make deploy-size-calculator` then make [this](https://github.com/grafana/loki/blob/main/operator/hack/deploy-prometheus-secret.sh) file executable by running:

```console
chmod +x hack/deploy-prometheus-secret.sh
```

Now rerun the same `make deploy-size-calculator` again and it should work fine.
