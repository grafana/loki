---
title: "API"
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
* [AlertingRule](#alertingrule)
* [AlertingRuleGroup](#alertingrulegroup)
* [AlertingRuleGroupSpec](#alertingrulegroupspec)
* [AlertingRuleList](#alertingrulelist)
* [AlertingRuleSpec](#alertingrulespec)
* [AlertingRuleStatus](#alertingrulestatus)
* [AuthenticationSpec](#authenticationspec)
* [AuthorizationSpec](#authorizationspec)
* [IngestionLimitSpec](#ingestionlimitspec)
* [LimitsSpec](#limitsspec)
* [LimitsTemplateSpec](#limitstemplatespec)
* [LokiComponentSpec](#lokicomponentspec)
* [LokiStack](#lokistack)
* [LokiStackComponentStatus](#lokistackcomponentstatus)
* [LokiStackList](#lokistacklist)
* [LokiStackSpec](#lokistackspec)
* [LokiStackStatus](#lokistackstatus)
* [LokiStackStorageStatus](#lokistackstoragestatus)
* [LokiTemplateSpec](#lokitemplatespec)
* [OIDCSpec](#oidcspec)
* [OPASpec](#opaspec)
* [ObjectStorageSchema](#objectstorageschema)
* [ObjectStorageSecretSpec](#objectstoragesecretspec)
* [ObjectStorageSpec](#objectstoragespec)
* [ObjectStorageTLSSpec](#objectstoragetlsspec)
* [QueryLimitSpec](#querylimitspec)
* [RoleBindingsSpec](#rolebindingsspec)
* [RoleSpec](#rolespec)
* [RulesSpec](#rulesspec)
* [Subject](#subject)
* [TenantSecretSpec](#tenantsecretspec)
* [TenantsSpec](#tenantsspec)
* [RecordingRule](#recordingrule)
* [RecordingRuleGroup](#recordingrulegroup)
* [RecordingRuleGroupSpec](#recordingrulegroupspec)
* [RecordingRuleList](#recordingrulelist)
* [RecordingRuleSpec](#recordingrulespec)
* [RecordingRuleStatus](#recordingrulestatus)
* [AlertManagerDiscoverySpec](#alertmanagerdiscoveryspec)
* [AlertManagerNotificationQueueSpec](#alertmanagernotificationqueuespec)
* [AlertManagerSpec](#alertmanagerspec)
* [RelabelConfig](#relabelconfig)
* [RemoteWriteClientQueueSpec](#remotewriteclientqueuespec)
* [RemoteWriteClientSpec](#remotewriteclientspec)
* [RemoteWriteSpec](#remotewritespec)
* [RulerConfig](#rulerconfig)
* [RulerConfigList](#rulerconfiglist)
* [RulerConfigSpec](#rulerconfigspec)
* [RulerConfigStatus](#rulerconfigstatus)

## AlertingRule

AlertingRule is the Schema for the alertingrules API


<em>appears in: [AlertingRuleList](#alertingrulelist)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#objectmeta-v1-meta) | false |
| spec |  | [AlertingRuleSpec](#alertingrulespec) | false |
| status |  | [AlertingRuleStatus](#alertingrulestatus) | false |

[Back to TOC](#table-of-contents)

## AlertingRuleGroup

AlertingRuleGroup defines a group of Loki alerting rules.


<em>appears in: [AlertingRuleSpec](#alertingrulespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name | Name of the alerting rule group. Must be unique within all alerting rules. | string | true |
| interval | Interval defines the time interval between evaluation of the given alerting rule. | PrometheusDuration | true |
| limit | Limit defines the number of alerts an alerting rule can produce. 0 is no limit. | int32 | false |
| rules | Rules defines a list of alerting rules | []*[AlertingRuleGroupSpec](#alertingrulegroupspec) | true |

[Back to TOC](#table-of-contents)

## AlertingRuleGroupSpec

AlertingRuleGroupSpec defines the spec for a Loki alerting rule.


<em>appears in: [AlertingRuleGroup](#alertingrulegroup)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| alert | The name of the alert. Must be a valid label value. | string | false |
| expr | The LogQL expression to evaluate. Every evaluation cycle this is evaluated at the current time, and all resultant time series become pending/firing alerts. | string | true |
| for | Alerts are considered firing once they have been returned for this long. Alerts which have not yet fired for long enough are considered pending. | PrometheusDuration | false |
| annotations | Annotations to add to each alert. | map[string]string | false |
| labels | Labels to add to each alert. | map[string]string | false |

[Back to TOC](#table-of-contents)

## AlertingRuleList

AlertingRuleList contains a list of AlertingRule

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#listmeta-v1-meta) | false |
| items |  | [][AlertingRule](#alertingrule) | true |

[Back to TOC](#table-of-contents)

## AlertingRuleSpec

AlertingRuleSpec defines the desired state of AlertingRule


<em>appears in: [AlertingRule](#alertingrule)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| tenantID | TenantID of tenant where the alerting rules are evaluated in. | string | true |
| groups | List of groups for alerting rules. | []*[AlertingRuleGroup](#alertingrulegroup) | true |

[Back to TOC](#table-of-contents)

## AlertingRuleStatus

AlertingRuleStatus defines the observed state of AlertingRule


<em>appears in: [AlertingRule](#alertingrule)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| conditions | Conditions of the AlertingRule generation health. | []metav1.Condition | false |

[Back to TOC](#table-of-contents)

## AuthenticationSpec

AuthenticationSpec defines the oidc configuration per tenant for lokiStack Gateway component.


<em>appears in: [TenantsSpec](#tenantsspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| tenantName | TenantName defines the name of the tenant. | string | true |
| tenantId | TenantID defines the id of the tenant. | string | true |
| oidc | OIDC defines the spec for the OIDC tenant's authentication. | *[OIDCSpec](#oidcspec) | true |

[Back to TOC](#table-of-contents)

## AuthorizationSpec

AuthorizationSpec defines the opa, role bindings and roles configuration per tenant for lokiStack Gateway component.


<em>appears in: [TenantsSpec](#tenantsspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| opa | OPA defines the spec for the third-party endpoint for tenant's authorization. | *[OPASpec](#opaspec) | true |
| roles | Roles defines a set of permissions to interact with a tenant. | [][RoleSpec](#rolespec) | true |
| roleBindings | RoleBindings defines configuration to bind a set of roles to a set of subjects. | [][RoleBindingsSpec](#rolebindingsspec) | true |

[Back to TOC](#table-of-contents)

## IngestionLimitSpec

IngestionLimitSpec defines the limits applied at the ingestion path.


<em>appears in: [LimitsTemplateSpec](#limitstemplatespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| ingestionRate | IngestionRate defines the sample size per second. Units MB. | int32 | false |
| ingestionBurstSize | IngestionBurstSize defines the local rate-limited sample size per distributor replica. It should be set to the set at least to the maximum logs size expected in a single push request. | int32 | false |
| maxLabelNameLength | MaxLabelNameLength defines the maximum number of characters allowed for label keys in log streams. | int32 | false |
| maxLabelValueLength | MaxLabelValueLength defines the maximum number of characters allowed for label values in log streams. | int32 | false |
| maxLabelNamesPerSeries | MaxLabelNamesPerSeries defines the maximum number of label names per series in each log stream. | int32 | false |
| maxGlobalStreamsPerTenant | MaxGlobalStreamsPerTenant defines the maximum number of active streams per tenant, across the cluster. | int32 | false |
| maxLineSize | MaxLineSize defines the maximum line size on ingestion path. Units in Bytes. | int32 | false |

[Back to TOC](#table-of-contents)

## LimitsSpec

LimitsSpec defines the spec for limits applied at ingestion or query path across the cluster or per tenant.


<em>appears in: [LokiStackSpec](#lokistackspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| global | Global defines the limits applied globally across the cluster. | *[LimitsTemplateSpec](#limitstemplatespec) | false |
| tenants | Tenants defines the limits applied per tenant. | map[string][LimitsTemplateSpec](#limitstemplatespec) | false |

[Back to TOC](#table-of-contents)

## LimitsTemplateSpec

LimitsTemplateSpec defines the limits  applied at ingestion or query path.


<em>appears in: [LimitsSpec](#limitsspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| ingestion | IngestionLimits defines the limits applied on ingested log streams. | *[IngestionLimitSpec](#ingestionlimitspec) | false |
| queries | QueryLimits defines the limit applied on querying log streams. | *[QueryLimitSpec](#querylimitspec) | false |

[Back to TOC](#table-of-contents)

## LokiComponentSpec

LokiComponentSpec defines the requirements to configure scheduling of each loki component individually.


<em>appears in: [LokiTemplateSpec](#lokitemplatespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| replicas | Replicas defines the number of replica pods of the component. | int32 | false |
| nodeSelector | NodeSelector defines the labels required by a node to schedule the component onto it. | map[string]string | false |
| tolerations | Tolerations defines the tolerations required by a node to schedule the component onto it. | []corev1.Toleration | false |

[Back to TOC](#table-of-contents)

## LokiStack

LokiStack is the Schema for the lokistacks API


<em>appears in: [LokiStackList](#lokistacklist)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| spec |  | [LokiStackSpec](#lokistackspec) | false |
| status |  | [LokiStackStatus](#lokistackstatus) | false |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#objectmeta-v1-meta) | false |

[Back to TOC](#table-of-contents)

## LokiStackComponentStatus

LokiStackComponentStatus defines the map of per pod status per LokiStack component. Each component is represented by a separate map of v1.Phase to a list of pods.


<em>appears in: [LokiStackStatus](#lokistackstatus)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| compactor | Compactor is a map to the pod status of the compactor pod. | PodStatusMap | false |
| distributor | Distributor is a map to the per pod status of the distributor deployment | PodStatusMap | false |
| indexGateway | IndexGateway is a map to the per pod status of the index gateway statefulset | PodStatusMap | false |
| ingester | Ingester is a map to the per pod status of the ingester statefulset | PodStatusMap | false |
| querier | Querier is a map to the per pod status of the querier deployment | PodStatusMap | false |
| queryFrontend | QueryFrontend is a map to the per pod status of the query frontend deployment | PodStatusMap | false |
| gateway | Gateway is a map to the per pod status of the lokistack gateway deployment. | PodStatusMap | false |
| ruler | Ruler is a map to the per pod status of the lokistack ruler statefulset. | PodStatusMap | false |

[Back to TOC](#table-of-contents)

## LokiStackList

LokiStackList contains a list of LokiStack

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#listmeta-v1-meta) | false |
| items |  | [][LokiStack](#lokistack) | true |

[Back to TOC](#table-of-contents)

## LokiStackSpec

LokiStackSpec defines the desired state of LokiStack


<em>appears in: [LokiStack](#lokistack)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| managementState | ManagementState defines if the CR should be managed by the operator or not. Default is managed. | ManagementStateType | false |
| size | Size defines one of the support Loki deployment scale out sizes. | LokiStackSizeType | true |
| storage | Storage defines the spec for the object storage endpoint to store logs. | [ObjectStorageSpec](#objectstoragespec) | true |
| storageClassName | Storage class name defines the storage class for ingester/querier PVCs. | string | true |
| replicationFactor | ReplicationFactor defines the policy for log stream replication. | int32 | true |
| rules | Rules defines the spec for the ruler component | *[RulesSpec](#rulesspec) | false |
| limits | Limits defines the limits to be applied to log stream processing. | *[LimitsSpec](#limitsspec) | false |
| template | Template defines the resource/limits/tolerations/nodeselectors per component | *[LokiTemplateSpec](#lokitemplatespec) | false |
| tenants | Tenants defines the per-tenant authentication and authorization spec for the lokistack-gateway component. | *[TenantsSpec](#tenantsspec) | false |

[Back to TOC](#table-of-contents)

## LokiStackStatus

LokiStackStatus defines the observed state of LokiStack


<em>appears in: [LokiStack](#lokistack)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| components | Components provides summary of all Loki pod status grouped per component. | [LokiStackComponentStatus](#lokistackcomponentstatus) | false |
| storage | Storage provides summary of all changes that have occurred to the storage configuration. | [LokiStackStorageStatus](#lokistackstoragestatus) | false |
| conditions | Conditions of the Loki deployment health. | []metav1.Condition | false |

[Back to TOC](#table-of-contents)

## LokiStackStorageStatus

LokiStackStorageStatus defines the observed state of the Loki storage configuration.


<em>appears in: [LokiStackStatus](#lokistackstatus)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| schemas | Schemas is a list of schemas which have been applied to the LokiStack. | [][ObjectStorageSchema](#objectstorageschema) | false |

[Back to TOC](#table-of-contents)

## LokiTemplateSpec

LokiTemplateSpec defines the template of all requirements to configure scheduling of all Loki components to be deployed.


<em>appears in: [LokiStackSpec](#lokistackspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| compactor | Compactor defines the compaction component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| distributor | Distributor defines the distributor component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| ingester | Ingester defines the ingester component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| querier | Querier defines the querier component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| queryFrontend | QueryFrontend defines the query frontend component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| gateway | Gateway defines the lokistack gateway component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| indexGateway | IndexGateway defines the index gateway component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |
| ruler | Ruler defines the ruler component spec. | *[LokiComponentSpec](#lokicomponentspec) | false |

[Back to TOC](#table-of-contents)

## OIDCSpec

OIDCSpec defines the oidc configuration spec for lokiStack Gateway component.


<em>appears in: [AuthenticationSpec](#authenticationspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| secret | Secret defines the spec for the clientID, clientSecret and issuerCAPath for tenant's authentication. | *[TenantSecretSpec](#tenantsecretspec) | true |
| issuerURL | IssuerURL defines the URL for issuer. | string | true |
| redirectURL | RedirectURL defines the URL for redirect. | string | false |
| groupClaim | Group claim field from ID Token | string | false |
| usernameClaim | User claim field from ID Token | string | false |

[Back to TOC](#table-of-contents)

## OPASpec

OPASpec defines the opa configuration spec for lokiStack Gateway component.


<em>appears in: [AuthorizationSpec](#authorizationspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| url | URL defines the third-party endpoint for authorization. | string | true |

[Back to TOC](#table-of-contents)

## ObjectStorageSchema

ObjectStorageSchema defines the requirements needed to configure a new storage schema.


<em>appears in: [LokiStackStorageStatus](#lokistackstoragestatus), [ObjectStorageSpec](#objectstoragespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| version | Version for writing and reading logs. | ObjectStorageSchemaVersion | true |
| effectiveDate | EffectiveDate is the date in UTC that the schema will be applied on. To ensure readibility of logs, this date should be before the current date in UTC. | StorageSchemaEffectiveDate | true |

[Back to TOC](#table-of-contents)

## ObjectStorageSecretSpec

ObjectStorageSecretSpec is a secret reference containing name only, no namespace.


<em>appears in: [ObjectStorageSpec](#objectstoragespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| type | Type of object storage that should be used | ObjectStorageSecretType | true |
| name | Name of a secret in the namespace configured for object storage secrets. | string | true |

[Back to TOC](#table-of-contents)

## ObjectStorageSpec

ObjectStorageSpec defines the requirements to access the object storage bucket to persist logs by the ingester component.


<em>appears in: [LokiStackSpec](#lokistackspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| schemas | Schemas for reading and writing logs. | [][ObjectStorageSchema](#objectstorageschema) | true |
| secret | Secret for object storage authentication. Name of a secret in the same namespace as the LokiStack custom resource. | [ObjectStorageSecretSpec](#objectstoragesecretspec) | true |
| tls | TLS configuration for reaching the object storage endpoint. | *[ObjectStorageTLSSpec](#objectstoragetlsspec) | false |

[Back to TOC](#table-of-contents)

## ObjectStorageTLSSpec

ObjectStorageTLSSpec is the TLS configuration for reaching the object storage endpoint.


<em>appears in: [ObjectStorageSpec](#objectstoragespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| caName | CA is the name of a ConfigMap containing a CA certificate. It needs to be in the same namespace as the LokiStack custom resource. | string | false |

[Back to TOC](#table-of-contents)

## QueryLimitSpec

QueryLimitSpec defines the limits applies at the query path.


<em>appears in: [LimitsTemplateSpec](#limitstemplatespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| maxEntriesLimitPerQuery | MaxEntriesLimitsPerQuery defines the maximum number of log entries that will be returned for a query. | int32 | false |
| maxChunksPerQuery | MaxChunksPerQuery defines the maximum number of chunks that can be fetched by a single query. | int32 | false |
| maxQuerySeries | MaxQuerySeries defines the the maximum of unique series that is returned by a metric query. | int32 | false |

[Back to TOC](#table-of-contents)

## RoleBindingsSpec

RoleBindingsSpec binds a set of roles to a set of subjects.


<em>appears in: [AuthorizationSpec](#authorizationspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name |  | string | true |
| subjects |  | [][Subject](#subject) | true |
| roles |  | []string | true |

[Back to TOC](#table-of-contents)

## RoleSpec

RoleSpec describes a set of permissions to interact with a tenant.


<em>appears in: [AuthorizationSpec](#authorizationspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name |  | string | true |
| resources |  | []string | true |
| tenants |  | []string | true |
| permissions |  | []PermissionType | true |

[Back to TOC](#table-of-contents)

## RulesSpec

RulesSpec deifnes the spec for the ruler component.


<em>appears in: [LokiStackSpec](#lokistackspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enabled | Enabled defines a flag to enable/disable the ruler component | bool | true |
| selector | A selector to select which LokiRules to mount for loading alerting/recording rules from. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#labelselector-v1-meta) | false |
| namespaceSelector | Namespaces to be selected for PrometheusRules discovery. If unspecified, only the same namespace as the LokiStack object is in is used. | *[metav1.LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#labelselector-v1-meta) | false |

[Back to TOC](#table-of-contents)

## Subject

Subject represents a subject that has been bound to a role.


<em>appears in: [RoleBindingsSpec](#rolebindingsspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name |  | string | true |
| kind |  | SubjectKind | true |

[Back to TOC](#table-of-contents)

## TenantSecretSpec

TenantSecretSpec is a secret reference containing name only for a secret living in the same namespace as the LokiStack custom resource.


<em>appears in: [OIDCSpec](#oidcspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name | Name of a secret in the namespace configured for tenant secrets. | string | true |

[Back to TOC](#table-of-contents)

## TenantsSpec

TenantsSpec defines the mode, authentication and authorization configuration of the lokiStack gateway component.


<em>appears in: [LokiStackSpec](#lokistackspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| mode | Mode defines the mode in which lokistack-gateway component will be configured. | ModeType | true |
| authentication | Authentication defines the lokistack-gateway component authentication configuration spec per tenant. | [][AuthenticationSpec](#authenticationspec) | false |
| authorization | Authorization defines the lokistack-gateway component authorization configuration spec per tenant. | *[AuthorizationSpec](#authorizationspec) | false |

[Back to TOC](#table-of-contents)

## RecordingRule

RecordingRule is the Schema for the recordingrules API


<em>appears in: [RecordingRuleList](#recordingrulelist)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#objectmeta-v1-meta) | false |
| spec |  | [RecordingRuleSpec](#recordingrulespec) | false |
| status |  | [RecordingRuleStatus](#recordingrulestatus) | false |

[Back to TOC](#table-of-contents)

## RecordingRuleGroup

RecordingRuleGroup defines a group of Loki  recording rules.


<em>appears in: [RecordingRuleSpec](#recordingrulespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name | Name of the recording rule group. Must be unique within all recording rules. | string | true |
| interval | Interval defines the time interval between evaluation of the given recoding rule. | PrometheusDuration | true |
| limit | Limit defines the number of series a recording rule can produce. 0 is no limit. | int32 | false |
| rules | Rules defines a list of recording rules | []*[RecordingRuleGroupSpec](#recordingrulegroupspec) | true |

[Back to TOC](#table-of-contents)

## RecordingRuleGroupSpec

RecordingRuleGroupSpec defines the spec for a Loki recording rule.


<em>appears in: [RecordingRuleGroup](#recordingrulegroup)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| record | The name of the time series to output to. Must be a valid metric name. | string | false |
| expr | The LogQL expression to evaluate. Every evaluation cycle this is evaluated at the current time, and all resultant time series become pending/firing alerts. | string | true |

[Back to TOC](#table-of-contents)

## RecordingRuleList

RecordingRuleList contains a list of RecordingRule

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#listmeta-v1-meta) | false |
| items |  | [][RecordingRule](#recordingrule) | true |

[Back to TOC](#table-of-contents)

## RecordingRuleSpec

RecordingRuleSpec defines the desired state of RecordingRule


<em>appears in: [RecordingRule](#recordingrule)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| tenantID | TenantID of tenant where the recording rules are evaluated in. | string | true |
| groups | List of groups for recording rules. | []*[RecordingRuleGroup](#recordingrulegroup) | true |

[Back to TOC](#table-of-contents)

## RecordingRuleStatus

RecordingRuleStatus defines the observed state of RecordingRule


<em>appears in: [RecordingRule](#recordingrule)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| conditions | Conditions of the RecordingRule generation health. | []metav1.Condition | false |

[Back to TOC](#table-of-contents)

## AlertManagerDiscoverySpec

AlertManagerDiscoverySpec defines the configuration to use DNS resolution for AlertManager hosts.


<em>appears in: [AlertManagerSpec](#alertmanagerspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enableSRV | Use DNS SRV records to discover Alertmanager hosts. | bool | true |
| refreshInterval | How long to wait between refreshing DNS resolutions of Alertmanager hosts. | PrometheusDuration | false |

[Back to TOC](#table-of-contents)

## AlertManagerNotificationQueueSpec

AlertManagerNotificationQueueSpec defines the configuration for AlertManager notification settings.


<em>appears in: [AlertManagerSpec](#alertmanagerspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| capacity | Capacity of the queue for notifications to be sent to the Alertmanager. | int32 | false |
| timeout | HTTP timeout duration when sending notifications to the Alertmanager. | PrometheusDuration | false |
| forOutageTolerance | Max time to tolerate outage for restoring \"for\" state of alert. | PrometheusDuration | false |
| forGracePeriod | Minimum duration between alert and restored \"for\" state. This is maintained only for alerts with configured \"for\" time greater than the grace period. | PrometheusDuration | false |
| resendDelay | Minimum amount of time to wait before resending an alert to Alertmanager. | PrometheusDuration | false |

[Back to TOC](#table-of-contents)

## AlertManagerSpec

AlertManagerSpec defines the configuration for ruler's alertmanager connectivity.


<em>appears in: [RulerConfigSpec](#rulerconfigspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| externalUrl | URL for alerts return path. | string | false |
| externalLabels | Additional labels to add to all alerts. | map[string]string | false |
| enableV2 | If enabled, then requests to Alertmanager use the v2 API. | bool | true |
| endpoints | List of AlertManager URLs to send notifications to. Each Alertmanager URL is treated as a separate group in the configuration. Multiple Alertmanagers in HA per group can be supported by using DNS resolution (See EnableDNSDiscovery). | []string | true |
| discovery | Defines the configuration for DNS-based discovery of AlertManager hosts. | *[AlertManagerDiscoverySpec](#alertmanagerdiscoveryspec) | false |
| notificationQueue | Defines the configuration for the notification queue to AlertManager hosts. | *[AlertManagerNotificationQueueSpec](#alertmanagernotificationqueuespec) | false |

[Back to TOC](#table-of-contents)

## RelabelConfig

RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion. It defines `<metric_relabel_configs>`-section of Prometheus configuration. More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs


<em>appears in: [RemoteWriteClientSpec](#remotewriteclientspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| sourceLabels | The source labels select values from existing labels. Their content is concatenated using the configured separator and matched against the configured regular expression for the replace, keep, and drop actions. | []string | true |
| separator | Separator placed between concatenated source label values. default is ';'. | string | false |
| targetLabel | Label to which the resulting value is written in a replace action. It is mandatory for replace actions. Regex capture groups are available. | string | false |
| regex | Regular expression against which the extracted value is matched. Default is '(.*)' | string | false |
| modulus | Modulus to take of the hash of the source label values. | uint64 | false |
| replacement | Replacement value against which a regex replace is performed if the regular expression matches. Regex capture groups are available. Default is '$1' | string | false |
| action | Action to perform based on regex matching. Default is 'replace' | RelabelActionType | false |

[Back to TOC](#table-of-contents)

## RemoteWriteClientQueueSpec

RemoteWriteClientQueueSpec defines the configuration of the remote write client queue.


<em>appears in: [RemoteWriteSpec](#remotewritespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| capacity | Number of samples to buffer per shard before we block reading of more | int32 | false |
| maxShards | Maximum number of shards, i.e. amount of concurrency. | int32 | false |
| minShards | Minimum number of shards, i.e. amount of concurrency. | int32 | false |
| maxSamplesPerSend | Maximum number of samples per send. | int32 | false |
| batchSendDeadline | Maximum time a sample will wait in buffer. | PrometheusDuration | false |
| minBackOffPeriod | Initial retry delay. Gets doubled for every retry. | PrometheusDuration | false |
| maxBackOffPeriod | Maximum retry delay. | PrometheusDuration | false |

[Back to TOC](#table-of-contents)

## RemoteWriteClientSpec

RemoteWriteClientSpec defines the configuration of the remote write client.


<em>appears in: [RemoteWriteSpec](#remotewritespec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| name | Name of the remote write config, which if specified must be unique among remote write configs. | string | true |
| url | The URL of the endpoint to send samples to. | string | true |
| timeout | Timeout for requests to the remote write endpoint. | PrometheusDuration | false |
| authorization | Type of authorzation to use to access the remote write endpoint | RemoteWriteAuthType | true |
| authorizationSecretName | Name of a secret in the namespace configured for authorization secrets. | string | true |
| additionalHeaders | Additional HTTP headers to be sent along with each remote write request. | map[string]string | false |
| relabelConfigs | List of remote write relabel configurations. | [][RelabelConfig](#relabelconfig) | false |
| proxyUrl | Optional proxy URL. | string | false |
| followRedirects | Configure whether HTTP requests follow HTTP 3xx redirects. | bool | true |

[Back to TOC](#table-of-contents)

## RemoteWriteSpec

RemoteWriteSpec defines the configuration for ruler's remote_write connectivity.


<em>appears in: [RulerConfigSpec](#rulerconfigspec)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enabled | Enable remote-write functionality. | bool | false |
| refreshPeriod | Minimum period to wait between refreshing remote-write reconfigurations. | PrometheusDuration | false |
| client | Defines the configuration for remote write client. | *[RemoteWriteClientSpec](#remotewriteclientspec) | false |
| queue | Defines the configuration for remote write client queue. | *[RemoteWriteClientQueueSpec](#remotewriteclientqueuespec) | false |

[Back to TOC](#table-of-contents)

## RulerConfig

RulerConfig is the Schema for the rulerconfigs API


<em>appears in: [RulerConfigList](#rulerconfiglist)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#objectmeta-v1-meta) | false |
| spec |  | [RulerConfigSpec](#rulerconfigspec) | false |
| status |  | [RulerConfigStatus](#rulerconfigstatus) | false |

[Back to TOC](#table-of-contents)

## RulerConfigList

RulerConfigList contains a list of RuleConfig

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.23/#listmeta-v1-meta) | false |
| items |  | [][RulerConfig](#rulerconfig) | true |

[Back to TOC](#table-of-contents)

## RulerConfigSpec

RulerConfigSpec defines the desired state of Ruler


<em>appears in: [RulerConfig](#rulerconfig)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| evaluationInterval | Interval on how frequently to evaluate rules. | PrometheusDuration | false |
| pollInterval | Interval on how frequently to poll for new rule definitions. | PrometheusDuration | false |
| alertmanager | Defines alert manager configuration to notify on firing alerts. | *[AlertManagerSpec](#alertmanagerspec) | false |
| remoteWrite | Defines a remote write endpoint to write recording rule metrics. | *[RemoteWriteSpec](#remotewritespec) | false |

[Back to TOC](#table-of-contents)

## RulerConfigStatus

RulerConfigStatus defines the observed state of RulerConfig


<em>appears in: [RulerConfig](#rulerconfig)</em>

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| conditions | Conditions of the RulerConfig health. | []metav1.Condition | false |

[Back to TOC](#table-of-contents)
