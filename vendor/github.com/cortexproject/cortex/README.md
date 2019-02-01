<p align="center"><img src="imgs/logo.png" alt="Cortex Logo"></p>

# Open source, horizontally scalable, multi-tenant Prometheus as a service

[![Circle CI](https://circleci.com/gh/cortexproject/cortex/tree/master.svg?style=shield)](https://circleci.com/gh/cortexproject/cortex/tree/master)
[![GoDoc](https://godoc.org/github.com/cortexproject/cortex?status.svg)](https://godoc.org/github.com/cortexproject/cortex)

Cortex is a [CNCF](https://cncf.io) sandbox project used in several production
systems including [Weave Cloud](https://cloud.weave.works), [GrafanaCloud](https://grafana.com/cloud)
and [FreshTracks.io](https://www.freshtracks.io/).

Cortex provides horizontally scalable, multi-tenant, long term storage for
[Prometheus](https://prometheus.io) metrics when used as a [remote
write](https://prometheus.io/docs/operating/configuration/#remote_write)
destination, and a horizontally scalable, Prometheus-compatible query
API.

Multi-tenant means it handles metrics from multiple independent
Prometheus sources, keeping them separate.

[Instrumenting Your App: Best Practises](https://www.weave.works/docs/cloud/latest/concepts/prometheus-monitoring/)

## Hosted Cortex (Prometheus as a service)

There are several commercial services where you can use Cortex
on-demand:

### Weave Cloud

[Weave Cloud](https://cloud.weave.works) from
[Weaveworks](https://weave.works) lets you deploy, manage, and monitor
container-based applications. Sign up at https://cloud.weave.works
and follow the instructions there. Additional help can also be found
in the [Weave Cloud documentation](https://www.weave.works/docs/cloud/latest/overview/).

### GrafanaCloud

To use Cortex as part of Grafana Cloud, sign up for [GrafanaCloud](https://grafana.com/cloud)
by clicking "Log In" in the top right and then "Sign Up Now".  Cortex is included
as part of the Starter and Basic Hosted Grafana plans.

## Contributing

For a guide to contributing to Cortex, see the [contributor guidelines](CONTRIBUTING.md).

## For Developers

To build:
```
make
```

(By default the build runs in a Docker container, using an image built
with all the tools required. The source code is mounted from where you
run `make` into the build container as a Docker volume.)

To run the test suite:
```
make test
```

To checkout Cortex in minikube:
```
kubectl create -f ./k8s
```

(these manifests use `latest` tags, i.e. this will work if you have
just built the images and they are available on the node(s) in your
Kubernetes cluster)

Cortex will sit behind an nginx instance exposed on port 30080.  A job is deployed to scrape it itself.  Try it:

http://192.168.99.100:30080/api/prom/api/v1/query?query=up

### Vendoring

We use `dep` to vendor dependencies.  To fetch a new dependency, run:

    dep ensure

To update dependencies, run

    dep ensure --update

## Further reading

To learn more about Cortex, consult the following documents / talks:

- [Original design document for Project Frankenstein](http://goo.gl/prdUYV)
- PromCon 2016 Talk: "Project Frankenstein: Multitenant, Scale-Out Prometheus": [video](https://youtu.be/3Tb4Wc0kfCM), [slides](http://www.slideshare.net/weaveworks/project-frankenstein-a-multitenant-horizontally-scalable-prometheus-as-a-service)
- KubeCon Prometheus Day talk "Weave Cortex: Multi-tenant, horizontally scalable Prometheus as a Service" [slides](http://www.slideshare.net/weaveworks/weave-cortex-multitenant-horizontally-scalable-prometheus-as-a-service)
- CNCF TOC Presentation; "Horizontally Scalable, Multi-tenant Prometheus" [slides](https://docs.google.com/presentation/d/190oIFgujktVYxWZLhLYN4q8p9dtQYoe4sxHgn4deBSI/edit#slide=id.g3b8e2d6f7e_0_6)

## <a name="help"></a>Getting Help

If you have any questions regarding Cortex:

- Ask a question on the [Cortex Slack channel](https://cloud-native.slack.com/messages/cortex/). To invite yourself to the CNCF Slack, visit http://slack.cncf.io/.
- <a href="https://github.com/cortexproject/cortex/issues/new">File an issue.</a>
- Send an email to <a href="mailto:cortex-users@lists.cncf.io">cortex-users@lists.cncf.io</a>

Your feedback is always welcome.
