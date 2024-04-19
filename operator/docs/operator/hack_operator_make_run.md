---
title: "Hacking using make run"
description: "Hacking using make run"
lead: ""
date: 2022-06-21T08:48:45+00:00
lastmod: 2022-06-21T08:48:45+00:00
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 100
toc: true
---

This document demonstrates how to use Loki Operator for development and testing locally on Kind and OpenShift using the `make run` command.

_Note:_ This is helpful when you don't want to deploy the Loki Operator image everytime you make slight changes to the operator code.

## Hacking using kind

[kind](https://kind.sigs.k8s.io/docs/user/quick-start/) is a tool for running local Kubernetes clusters using Docker container "nodes". kind was primarily designed for testing Kubernetes itself, but may be used for local development or CI.

### Requirements

* Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) or [Openshift CLI](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) for communicating with the cluster. The guide below will be using `kubectl` for the same.
* Create a running Kubernetes cluster using kind.

### Installation of Loki Operator

* Install the CRDs into the cluster:

  ```console
  make install
  ```

  This will create a custom resource definition with the name `lokistacks.loki.grafana.com` which can be verified using:

  ```console
  kubectl get crd lokistacks.loki.grafana.com
  ```

* Create a minio deployment in the cluster using:

  ```console
  kubectl apply -k config/overlays/development/minio
  ```

  This creates minio's  `deployment`, `service`, `pvc` and `secret` in the `default` namespace.

* Now create a LokiStack instance using:

  ```console
  kubectl apply -f hack/lokistack_dev.yaml
  ```

* Now run the operator locally on your machine:

  ```console
  make run
  ```

  This will start the loki operator locally, recognize the `LokiStack` CRD instance and also creates `distributor`, `compactor`, `ingester`, `querier` and `query-frontend` components.

  Confirm that all components are up and running for `deployments` using:

  ```console
  kubectl rollout status deployment/<DEPLOYMENT_NAME>  
  ```

  where `<DEPLOYMENT_NAME>` is the name of the deployment and can be found using:

  ```console  
  kubectl get deployments
  ```

  Confirm that all are up and running for `statefulsets` using:

  ```console
  kubectl rollout status statefulset/<STATEFULSET_NAME>
  ```

  where `<STATEFULSET_NAME>` is the name of the statefulset and can be found using:

  ```console
  kubectl get statefulsets
  ```

* If you make some changes to the operator's code, then just stop the operator locally using `CTRL + C`, update the code and rerun the operator locally:

  ```console
  make run
  ```

  This saves time by not deploying the operator again and again.

* When everything works fine, for the final testing deploy everything to the cluster using this [document](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#hacking-on-openshift).

### Cleanup

* Stop the operator from running locally by pressing `CTRL + C`.

* Cleanup the LokiStack instance, CRDs, deployments on the cluster using:

  ```console
  make uninstall
  ```

* Cleanup the minio deployment using:

  ```console
  kubectl delete -k config/overlays/development/minio
  ```

## Hacking on OpenShift

### Requirements

* Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) or [Openshift CLI](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) for communicating with the cluster. The guide below will be using `kubectl` for the same.
* Create a running OpenShift cluster on AWS.
* Create an [S3 bucket](https://docs.aws.amazon.com/AmazonS3/latest/userguide/create-bucket-overview.html) in one of the AWS Regions.

### Installation of Loki Operator

* Install the CRDs into the cluster:

  ```console
  make install
  ```

  This will create a custom resource definition with the name `lokistacks.loki.grafana.com` which can be verified using:

  ```console
  kubectl get crd lokistacks.loki.grafana.com
  ```

* Create the `openshift-logging` namespace in the cluster:

  ```console
  kubectl create ns openshift-logging
  ```

* Now you need to create a storage secret for the operator. This can be done using:

  ```console
  ./hack/deploy-aws-storage-secret.sh <BUCKET_NAME>
  ```

  This secret will be available in `openshift-logging` namespace. You can check the `hack/deploy-aws-storage-secret.sh` file to check the content of the secret. By default, the script will pull credential information using the `aws` cli. However, these values can be overwritten. For example:

  ```console
  REGION=us-west-1 ./hack/deploy-aws-storage-secret.sh <BUCKET_NAME>
  ```

* Once the object storage secret is created, you can now create a LokiStack instance:

  ```console
  kubectl -n openshift-logging apply -f hack/lokistack_dev.yaml
  ```

* Now run the operator locally on your machine:

  ```console
  make run
  ```

  This will create `distributor`, `compactor`, `ingester`, `querier` and `query-frontend` components only.

  Confirm that all are up and running for `deployments` using:

  ```console  
  kubectl -n openshift-logging rollout status deployment/<DEPLOYMENT_NAME>
  ```  

  where `<DEPLOYMENT_NAME>` is the name of the deployment and can be found using:

  ```console
  kubectl -n openshift-logging get deployments
  ```

  Confirm that all are up and running for `statefulsets` using:

  ```console  
  kubectl -n openshift-logging rollout status statefulset/<STATEFULSET_NAME>  
  ```  

  where `<STATEFULSET_NAME>` is the name of the statefulset and can be found using:

  ```console
  kubectl -n openshift-logging get statefulsets
  ```

* If you want `lokistack-gateway` component [1] to be deployed then you need to create a gateway secret [2] for the operator. This can be done using:

  ```code
  kubectl -n openshift-logging create secret generic test1 \
    --from-literal=clientID="<CLIENT_ID>" \
    --from-literal=clientSecret="<CLIENT_SECRET>" \
    --from-literal=issuerCAPath="<ISSUER_CA_PATH>"
  ```

* Now create a LokiStack instance using:

  ```console
  kubectl -n openshift-logging apply -f hack/lokistack_gateway_dev.yaml
  ```

* Edit the [main file](https://github.com/grafana/loki/blob/main/operator/main.go) to set the flag values to `true` and rerun the operator using:

  ```console
  make run
  ```

  This will create `distributor`, `compactor`, `ingester`, `querier`, `query-frontend` and `lokistack-gateway` components.

* If you make some changes to the operator's code, then just stop the operator locally using `CTRL + C`, update the code and rerun the operator locally:

  ```console
  make run
  ```

  This saves time by not deploying the operator again and again.

* When everything works fine, for the final testing deploy everything to the cluster using this [document](https://github.com/grafana/loki/blob/main/operator/docs/operator/hack_loki_operator.md#hacking-on-loki-operator-on-openshift).

### Cleanup

* Stop the operator from running locally by pressing `CTRL + C`.

* Cleanup the LokiStack instance, CRDs, deployments on the cluster using:

  ```console
  make uninstall
  ```

### Notes

[1] `lokistack-gateway` is an optional component deployed as part of Loki Operator. It provides secure access to Loki's distributor (i.e. for pushing logs) and query-frontend (i.e. for querying logs) via consulting an OAuth/OIDC endpoint for the request subject.

[2] The OIDC configuration expects `clientID`, `clientSecret` and `issuerCAPath` which should be provided via a Kubernetes secret that the LokiStack admin provides upfront.

Each tenant Secret is required to match:
* `metadata.name` with `TenantsSecretsSpec.Name`.
* `metadata.namespace` with `LokiStack.metadata.namespace`.

## Basic Troubleshooting on Hacking on Loki Operator

### kubectl using old context

It is possible that when you use two different clusters - one is kind cluster and the other is OpenShift cluster, you might need to switch between clusters to test your changes. There is a possibility that once you switch between clusters, the kubectl might not switch the context automatically and hence you might need to do this manually to correctly communicate with your cluster.

* List all the available context:

  ```console
  kubectl config get-contexts
  ```

  The `*` mark against the context shows the one in use currently.
* Set the context name you want to use now:

  ```console
  kubectl config use-context $CONTEXTNAME
  ```

  where `$CONTEXTNAME` is the context name you want to use now from the previous step.

### The Loki Operator giving Missing Secrets / Invalid Secrets error

You have probably forgotten to create the gateway secrets because of which the operator runs in **degraded** condition. Follow the steps mentioned in the step-by-step guide to create the gateway secret first. Once done, you can now create the LokiStack instance.

Verify this by checking the `conditions` field:

```console
kubectl get lokistack lokistack-dev -o yaml
```

For OpenShift, the above command would be:

```console
kubectl -n openshift-logging get lokistack lokistack-dev -o yaml
```

### The Loki Operator giving Mandatory Configuration / Incompatible Configuration error

This usually happens when the LokiStack CR is wrongly configured for the lokistack-gateway. Please read the [enhancement proposal](https://github.com/openshift/enhancements/blob/master/enhancements/cluster-logging/loki-gateway-configuration.md) to figure out the correct way to configure it.
