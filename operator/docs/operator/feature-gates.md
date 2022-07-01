---
title: "Feature Gates"
description: "Generated API docs for the Loki Operator"
lead: ""
date: 2021-03-08T08:49:31+00:00
draft: false
images: []
menu:
  docs:
    parent: "operator"
weight: 1000
toc: true
---

This Document documents the types introduced by the Loki Operator to be consumed by users.

> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [FeatureGates](#featuregates)
* [OpenShiftFeatureGates](#openshiftfeaturegates)
* [ProjectConfig](#projectconfig)

## FeatureGates

FeatureGates is the supported set of all operator feature gates.


<em>appears in: [ProjectConfig](#projectconfig)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| serviceMonitors | ServiceMonitors enables creating a Prometheus-Operator managed ServiceMonitor resource per LokiStack component. | bool | false |
| serviceMonitorTlsEndpoints | ServiceMonitorTLSEndpoints enables TLS for the ServiceMonitor endpoints. | bool | false |
| lokiStackAlerts | LokiStackAlerts enables creating Prometheus-Operator managed PrometheusRules for common Loki alerts. | bool | false |
| httpEncryption | HTTPEncryption enables TLS encryption for all HTTP LokiStack services. Each HTTP service requires a secret named as the service with the following data: - `tls.crt`: The TLS server side certificate. - `tls.key`: The TLS key for server-side encryption. In addition each service requires a configmap named as the LokiStack CR with the suffix `-ca-bundle`, e.g. `lokistack-dev-ca-bundle` and the following data: - `service-ca.crt`: The CA signing the service certificate in `tls.crt`. | bool | false |
| grpcEncryption | GRPCEncryption enables TLS encryption for all GRPC LokiStack services. Each GRPC service requires a secret named as the service with the following data: - `tls.crt`: The TLS server side certificate. - `tls.key`: The TLS key for server-side encryption. In addition each service requires a configmap named as the LokiStack CR with the suffix `-ca-bundle`, e.g. `lokistack-dev-ca-bundle` and the following data: - `service-ca.crt`: The CA signing the service certificate in `tls.crt`. | bool | false |
| lokiStackGateway | LokiStackGateway enables reconciling the reverse-proxy lokistack-gateway component for multi-tenant authentication/authorization traffic control to Loki. | bool | false |
| grafanaLabsUsageReport | GrafanaLabsUsageReport enables the Grafana Labs usage report for Loki. More details: https://grafana.com/docs/loki/latest/release-notes/v2-5/#usage-reporting | bool | false |
| runtimeSeccompProfile | RuntimeSeccompProfile enables the restricted seccomp profile on all Lokistack components. | bool | false |
| lokiStackWebhook | LokiStackWebhook enables the LokiStack CR validation and conversion webhooks. | bool | false |
| alertingRuleWebhook | AlertingRuleWebhook enables the AlertingRule CR validation webhook. | bool | false |
| recordingRuleWebhook | RecordingRuleWebhook enables the RecordingRule CR validation webhook. | bool | false |
| openshift | OpenShift contains a set of feature gates supported only on OpenShift. | [OpenShiftFeatureGates](#openshiftfeaturegates) | false |

[Back to TOC](#table-of-contents)

## OpenShiftFeatureGates

OpenShiftFeatureGates is the supported set of all operator features gates on OpenShift.


<em>appears in: [FeatureGates](#featuregates)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| servingCertsService | ServingCertsService enables OpenShift service-ca annotations on Services to use the in-platform CA and generate a TLS cert/key pair per service for in-cluster data-in-transit encryption. More details: https://docs.openshift.com/container-platform/latest/security/certificate_types_descriptions/service-ca-certificates.html | bool | false |
| gatewayRoute | GatewayRoute enables creating an OpenShift Route for the LokiStack gateway to expose the service to public internet access. More details: https://docs.openshift.com/container-platform/latest/networking/understanding-networking.html | bool | false |

[Back to TOC](#table-of-contents)

## ProjectConfig

ProjectConfig is the Schema for the projectconfigs API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| featureGates |  | [FeatureGates](#featuregates) | false |

[Back to TOC](#table-of-contents)
