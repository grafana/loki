---
title: "Hacking using an image registry"
description: "Hacking using an image registry"
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

Loki Operator is the Kubernetes Operator for [Loki](https://grafana.com/docs/loki/latest/).

## Hacking using kind

[kind](https://kind.sigs.k8s.io/docs/user/quick-start/) is a tool for running local Kubernetes clusters using Docker container "nodes". kind was primarily designed for testing Kubernetes itself, but may be used for local development or CI.

### Requirements

* Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) or [Openshift CLI](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) for communicating with the cluster. The guide below will be using `kubectl` for the same.
* Create a running Kubernetes cluster using kind.
* A container registry that you and your Kubernetes cluster can reach. We recommend  [quay.io](https://quay.io/signin/).

### Installation of Loki Operator

* Build and push the container image and then deploy the operator with:

  ```console
  make oci-build oci-push deploy REGISTRY_BASE=$YOUR_REPO_ORG VERSION=latest
  ```

  where `$YOUR_REPO_ORG` is your personal registry location, for example a [quay.io](http://quay.io/) account where you can push container images.

  The above command will deploy the operator to your active Kubernetes cluster defined by your local [kubeconfig](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/). The operator will be running in the `default` namespace.

* You can confirm that the operator is up and running using:

  ```console
  kubectl get pods
  ```

  You should see `controller-manager-xxxx` and `minio-xxxx` pods running.

* Now create a LokiStack instance to get the various components of Loki up and running:

  ```console
  kubectl apply -f hack/lokistack_dev.yaml
  ```

  This will create `distributor`, `compactor`, `ingester`, `querier` and `query-frontend` components.

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

### Cleanup

To cleanup deployments of the operator, you can use:

```console
make undeploy
```

It will undeploy controller from the configured Kubernetes cluster in [~/.kube/config](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/#the-kubeconfig-environment-variable)


## Hacking on OpenShift

### Requirements

* Install [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) or [Openshift CLI](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html) for communicating with the cluster. The guide below will be using `kubectl` for the same.
* Create a running OpenShift cluster on AWS.
* A container registry that you and your OpenShift cluster can reach. We recommend  [quay.io](https://quay.io/signin/).
* Create an [S3 bucket](https://docs.aws.amazon.com/AmazonS3/latest/userguide/create-bucket-overview.html) in one of the AWS Regions.

### Installation of Loki Operator

* Create the `openshift-operators-redhat` namespace in the cluster:

  ```console
  kubectl create ns openshift-operators-redhat
  ```

* Build and push the container image [2] and then deploy the operator with:

  ```console
  make olm-deploy REGISTRY_BASE=$YOUR_REPO_ORG VERSION=$VERSION
  ```

  where `$YOUR_REPO_ORG` is your personal registry location, for example a [quay.io](http://quay.io/) account where you can push container images and `$VERSION` can be any random version number such as `v0.0.1`.

  The above command will deploy the operator to your active Openshift cluster defined by your local [kubeconfig](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/). The operator will be running in the `openshift-operators-redhat` namespace.

* You can confirm that the operator is up and running using:

  ```console
  kubectl -n openshift-operators-redhat get pods
  ```

* Next step is to create the `openshift-logging` namespace in the cluster:

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

* Now you need to create a gateway secret [3] for the operator. This can be done using:

  ```code
  kubectl -n openshift-logging create secret generic test1 \
    --from-literal=clientID="<CLIENT_ID>" \
    --from-literal=clientSecret="<CLIENT_SECRET>" \
    --from-literal=issuerCAPath="<ISSUER_CA_PATH>"
  ```

* Once the object storage secret is created, you can now create a LokiStack instance to get the various components of Loki up and running:

  ```console
  kubectl -n openshift-logging apply -f hack/lokistack_gateway_dev.yaml
  ```

  This will create `distributor`, `compactor`, `ingester`, `querier`, `query-frontend` and `lokistack-gateway` components.

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

  If you don't want `lokistack-gateway` component [1] then you can skip it by removing the `--with-lokistack-gateway` args from the `loki-operator-controller-manager` deployment:

  ```console
  kubectl -n openshift-operators-redhat edit deployment/loki-operator-controller-manager
  ```

  Delete the flag `--with-lokistack-gateway` from the `args` section and save the file. This will update the deployment and now you can create LokiStack instance using:

  ```console
  kubectl -n openshift-logging apply -f hack/lokistack_dev.yaml
  ```

  This will create `distributor`, `compactor`, `ingester`, `querier` and `query-frontend` components only.

### Cleanup

To cleanup deployments of the operator, you can use:

```console
make olm-undeploy
```

It will cleanup deployments of the operator bundle, and the operator via OLM on an OpenShift cluster selected via [~/.kube/config](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/#the-kubeconfig-environment-variable)

### Notes

[1] `lokistack-gateway` is an optional component deployed as part of Loki Operator. It provides secure access to Loki's distributor (i.e. for pushing logs) and query-frontend (i.e. for querying logs) via consulting an OAuth/OIDC endpoint for the request subject.

[2] If you get multiple images as options, and you are required to select one of them then select `docker.io/library/golang:1.18`

[3] The OIDC configuration expects `clientID`, `clientSecret` and `issuerCAPath` which should be provided via a Kubernetes secret that the LokiStack admin provides upfront.

Each tenant Secret is required to match:
* `metadata.name` with `TenantsSecretsSpec.Name`.
* `metadata.namespace` with `LokiStack.metadata.namespace`.

## Development Add-Ons

To help with testing and development, a [Promtail](https://grafana.com/docs/loki/latest/clients/promtail/) and [logcli](https://grafana.com/docs/loki/latest/getting-started/logcli/) deployment are available. The example file has been configured to work with the [lokistack-gateway](./forwarding_logs_to_gateway.md). In order to work without this component, change the URLs to use the `distributor` and `query-frontend` service respectively.

In order to deploy these resources, follow the above steps to deploy the operator and instance. Then, do the following command:

```console
kubectl apply -f ./hack/addons_dev.yaml
```

### Notes

[1] When using an OpenShift cluster, the `addons_ocp.yaml` should be used. In a native K8s cluster the `addons_dev.yaml` should be used. The OpenShift environment uses `SecurityContextConstraints` in order to limit or enable pod capabilities.

[2] When deploying on a native K8s cluster, ensure that the namespaces of the `ServiceAccount` in the `ClusterRoleBinding` objects are changed accordingly.

## Basic Troubleshooting

### New changes are not detected by Loki Operator

Suppose you made some changes to the Loki Operator's code and deployed it, but the changes are not visible when it runs. This happens when the deployment pulls the old image of the operator because of the `imagePullPolicy` being set to `IfNotPresent`. Therefore, you need to make some changes to make your deployment pull a new image always:

* Go to `config/manager/manager.yaml` file.
* Set the `imagePullPolicy` to `Always` i.e.,

  ```yaml
  imagePullPolicy: Always
  ```

* Deploy the operator again.

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
