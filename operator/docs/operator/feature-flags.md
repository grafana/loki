---
title: "Feature Flags"
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
* [FeatureFlags](#featureflags)
* [ProjectConfig](#projectconfig)

## FeatureFlags

FeatureFlags is a set of operator feature flags.


<em>appears in: [ProjectConfig](#projectconfig)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enableCertSigningService |  | bool | false |
| enableServiceMonitors |  | bool | false |
| enableTlsHttpServices |  | bool | false |
| enableTlsServiceMonitorConfig |  | bool | false |
| enableTlsGrpcServices |  | bool | false |
| enableLokiStackAlerts |  | bool | false |
| enableLokiStackGateway |  | bool | false |
| enableLokiStackGatewayRoute |  | bool | false |
| enableGrafanaLabsStats |  | bool | false |
| enableLokiStackWebhook |  | bool | false |
| enableAlertingRuleWebhook |  | bool | false |
| enableRecordingRuleWebhook |  | bool | false |

[Back to TOC](#table-of-contents)

## ProjectConfig

ProjectConfig is the Schema for the projectconfigs API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| featureFlags |  | [FeatureFlags](#featureflags) | false |

[Back to TOC](#table-of-contents)
