---
title: Quick Start
description: Quick Start
lead: ""
lastmod: "2021-03-08T08:48:57+00:00"
draft: false
images: []
menu:
  docs:
    parent: prologue
weight: 200
toc: true
---

One page summary on how to start with Loki Operator and LokiStack.

## Requirements

The easiest way to start with the Loki Operator is to use Kubernetes [kind](sigs.k8s.io/kind).

## Deploy from Github repository

The simplest form to deploy the Loki Operator and a LokiStack for demo purposes is:

```shell
make quickstart
```

If you want to test local changes from your repository fork, you need to provide an image registry organization that you own that has an image repository name `loki-operator`, e.g. `quay.io/my-company-org/loki-operator`. The command to use your custom images is:

```shell
make quickstart REGISTRY_BASE=quay.io/my-company-org
```

## Further reading

For detailed instructions on how to start hacking on the Loki Operator, please follow one of the two guides:
- [Hack using an image registry →]({{< ref "hack_loki_operator.md" >}})
- [Hack using make run →]({{< ref "hack_operator_make_run.md" >}})
