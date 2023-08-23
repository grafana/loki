---
title: "Compatibility"
description: "The Loki Operator supports a number of Kubernetes and Loki releases."
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

The Loki Operator supports a number of Kubernetes and Loki releases.

## Kubernetes

The Loki Operator uses client-go to communicate with Kubernetes clusters. The supported Kubernetes cluster version is determined by client-go. The compatibility matrix for client-go and Kubernetes clusters can be found [here](https://github.com/kubernetes/client-go#compatibility-matrix). All additional compatibility is only best effort, or happens to still/already be supported. The currently used client-go version is "v0.23.5".

Due to the use of CustomResourceDefinitions Kubernetes >= v1.7.0 is required.

Due to the use of apiextensions.k8s.io/v1 CustomResourceDefinitions, requires Kubernetes >= v1.16.0.

## Loki

The versions of Loki compatible to be run with the Loki Operator are:

* v2.7.1
* v2.7.2
* v2.7.3
* v2.7.4
* v2.8.0
* v2.8.3
