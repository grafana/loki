---
title: Migrate from the Loki Helm chart to the Community Helm chart
menuTitle: Migrate to Community Helm chart
description: Migrate from the Loki repository Helm chart (6.x) to the Grafana Community Helm chart (12.x).
weight: 900
keywords:
  - upgrade
  - migrate
  - community
  - helm
---

# Migrate from the Loki Helm chart to the Community Helm chart

The Loki Helm chart has moved from the [Loki repository](https://github.com/grafana/loki) to the [Grafana Community Helm Charts repository](https://github.com/grafana-community/helm-charts). Chart version 6.55.0 (appVersion 3.6.7) was the last release from the Loki repository. Chart version 12.0.0 (appVersion 3.7.1) is the current release from the community repository.

This guide walks you through migrating from 6.55.0 to 12.0.0, which spans six major chart versions (7 through 12), each with breaking changes.

{{< admonition type="caution" >}}
This migration spans six major versions with accumulated breaking changes. Test the upgrade in a non-production environment before applying it to production.
{{< /admonition >}}

{{< admonition type="warning" >}}
Grafana Enterprise Logs (GEL) support was removed from the community chart in version 8.0.0. If you are a GEL user, **do not migrate** to the community chart. The upstream `grafana/loki` chart remains available for GEL users. Consult your Grafana Labs account team about your migration options. Refer to the [migration announcement](https://github.com/grafana/loki/issues/20705) for details.
{{< /admonition >}}

## Prerequisites

Before you begin:

- **Kubernetes 1.25 or later** is required (enforced since chart version 7.0.0).
- **Helm 3.x** is required.
- Back up your current Helm release values:

  ```bash
  helm get values <RELEASE_NAME> -o yaml > backup-values.yaml
  ```

- Back up any persistent volumes and object store indexes.

## Step 1: Update your Helm repository

The chart is now published to a different Helm repository and OCI registry.

**Before** (6.x from the Loki repository):

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm upgrade <RELEASE_NAME> grafana/loki -f values.yaml
```

**After** (12.x from the community repository):

```bash
helm repo add grafana-community https://grafana-community.github.io/helm-charts
helm repo update
helm upgrade <RELEASE_NAME> grafana-community/loki -f values.yaml --version 12.0.0
```

Or using OCI:

```bash
helm upgrade <RELEASE_NAME> oci://ghcr.io/grafana-community/helm-charts/loki -f values.yaml --version 12.0.0
```

## Step 2: Update your values file for breaking changes

The following sections describe every breaking change between chart versions 6.55.0 and 12.0.0, grouped by the major version that introduced each change. Review each section and update your values file accordingly.

### 6.x to 7.0: Kubernetes API cleanup

Support for deprecated Kubernetes APIs and PodSecurityPolicy has been removed. Kubernetes 1.25 or later is now required.

**Remove** the following from your values file:

```yaml
rbac:
  pspEnabled: false        # remove this key
  pspAnnotations: {}       # remove this key
```

The `rbac` section in 12.0.0 only contains:

```yaml
rbac:
  sccEnabled: false
  sccAllowHostDirVolumePlugin: false
  namespaced: false
```

If you have custom Ingress resources using `networking.k8s.io/v1beta1`, update them to `networking.k8s.io/v1`. If you reference PodDisruptionBudget resources with `policy/v1beta1`, update them to `policy/v1`.

### 7.x to 8.0: Enterprise support removed

The entire `enterprise` configuration section has been removed from the chart. If your values file includes any `enterprise.*` keys, remove them:

```yaml
# Remove this entire section
enterprise:
  enabled: false
  version: 3.6.5
  cluster_name: null
  license:
    contents: "NOTAVALIDLICENSE"
  useExternalLicense: false
  externalLicenseName: null
  externalConfigName: ""
  gelGateway: true
  # ... all other enterprise.* keys
```

### 8.x to 9.0: Self-monitoring and Grafana Agent Operator removed

The `monitoring.selfMonitoring` section and the `grafana-agent-operator` subchart dependency have been removed.

**Remove** the following from your values file:

```yaml
# Remove this entire section
monitoring:
  selfMonitoring:
    enabled: false
    tenant:
      name: "self-monitoring"
      password: null
      secretNamespace: '{{ include "loki.namespace" . }}'
    grafanaAgent:
      installOperator: false
      annotations: {}
      labels: {}
      enableConfigReadAPI: false
      priorityClassName: null
      resources: {}
      tolerations: []
    podLogs:
      apiVersion: monitoring.grafana.com/v1alpha1
      # ...
    logsInstance:
      # ...
```

Also remove `monitoring.serviceMonitor.metricsInstance` if present. It referenced a Grafana Agent CRD that no longer exists.

Loki canary tenant authentication has moved from `monitoring.selfMonitoring.tenant` to the top-level `lokiCanary.tenant` section. If you were using canary tenant auth, update as follows:

**Before** (6.x):

```yaml
monitoring:
  selfMonitoring:
    tenant:
      name: "self-monitoring"
      password: "my-password"
```

**After** (12.0.0):

```yaml
lokiCanary:
  tenant:
    name: "self-monitoring"
    password: "my-password"
```

{{< admonition type="tip" >}}
[Grafana Alloy](https://grafana.com/docs/alloy/latest/) is the successor to Grafana Agent. If you relied on Agent-based self-monitoring, consider using Alloy for log collection.
{{< /admonition >}}

### 9.x to 10.0: Index gateway persistence restructured

The `indexGateway.persistence.inMemory` field has been replaced with `indexGateway.persistence.dataVolumeParameters` for more consistent configuration across components.

**Before** (6.x):

```yaml
indexGateway:
  persistence:
    enabled: false
    inMemory: false
    size: 10Gi
```

**After** (12.0.0):

```yaml
indexGateway:
  persistence:
    enabled: false
    dataVolumeParameters:
      emptyDir: {}
    size: 10Gi
```

If you were using in-memory storage (`inMemory: true`), the equivalent configuration is:

```yaml
indexGateway:
  persistence:
    dataVolumeParameters:
      emptyDir:
        medium: Memory
        sizeLimit: 10Gi
```

### 10.x to 11.0: Legacy read target removed

The `read.legacyReadTarget` option has been removed. Simple scalable deployments now always require a dedicated backend target (read, write, and backend).

**Remove** from your values file if present:

```yaml
read:
  legacyReadTarget: false  # remove this key
```

If you had `legacyReadTarget: true` (running the 2-target read/write mode without a backend), you must now run the 3-target mode (read, write, backend) or switch to a different deployment mode.

### 11.x to 12.0: Deployment mode renamed and default changed

The default `deploymentMode` has changed from `SimpleScalable` to `Monolithic`, and `SingleBinary` has been renamed to `Monolithic`.

**Before** (6.x default):

```yaml
deploymentMode: SimpleScalable
```

**After** (12.0.0 default):

```yaml
deploymentMode: Monolithic
```

**Action required based on your current mode:**

If you are running **SimpleScalable** (the 6.x default), explicitly set it in your values file to preserve your current deployment:

```yaml
deploymentMode: SimpleScalable
```

{{< admonition type="note" >}}
`SimpleScalable` is deprecated and will be removed in Loki 4.0. Plan to migrate to `Monolithic` or `Distributed`.
{{< /admonition >}}

If you are running **SingleBinary**, update the value to the new name:

```yaml
# Before
deploymentMode: SingleBinary

# After
deploymentMode: Monolithic
```

If you are running **Distributed**, no change is needed.

## Step 3: Review additional deprecations

Several per-component fields have been deprecated in favor of unified blocks. These still work but will be removed in a future release. Consider updating them now:

```yaml
# Deprecated per-component service fields (applies to indexGateway, compactor, and others)
indexGateway:
  serviceLabels: {}        # deprecated, use indexGateway.service.labels
  serviceAnnotations: {}   # deprecated, use indexGateway.service.annotations
  serviceType: "ClusterIP" # deprecated, use indexGateway.service.type
  appProtocol:             # deprecated, use indexGateway.service.appProtocol
    grpc: ""
  maxUnavailable: null     # deprecated, use podDisruptionBudget.maxUnavailable
```

The global image registry override has also been renamed:

```yaml
# Deprecated
global:
  image:
    registry: null

# Use instead
global:
  imageRegistry: null
```

## Step 4: Perform the upgrade

After updating your values file, run the upgrade:

```bash
helm upgrade <RELEASE_NAME> grafana-community/loki \
  -f your-updated-values.yaml \
  --version 12.0.0
```

Or using OCI:

```bash
helm upgrade <RELEASE_NAME> oci://ghcr.io/grafana-community/helm-charts/loki \
  -f your-updated-values.yaml \
  --version 12.0.0
```

### Verify the upgrade

```bash
# Check all pods are running
kubectl get pods -l app.kubernetes.io/name=loki

# Verify Loki is accepting writes
kubectl logs -l app.kubernetes.io/component=write --tail=50

# Verify Loki canary is healthy (if enabled)
kubectl logs -l app.kubernetes.io/component=loki-canary --tail=20
```

## Step 5: Post-migration cleanup

After a successful upgrade:

- Remove the old Helm repository if no longer needed:

  ```bash
  helm repo remove grafana
  ```

- Update any CI/CD pipelines to reference `grafana-community/loki` or the OCI URL `oci://ghcr.io/grafana-community/helm-charts/loki`.
- Review monitoring dashboards for renamed metrics. Refer to the [Loki 3.x upgrade notes](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#30) for metric namespace changes.

## Troubleshooting

### Pods fail to start with PodSecurityPolicy errors

**Cause:** Leftover `rbac.pspEnabled` or `rbac.pspAnnotations` in your values file.

**Fix:** Remove both keys from your values file and re-run the upgrade.

### Loki canary fails authentication

**Cause:** Canary tenant configuration was under `monitoring.selfMonitoring.tenant` in 6.x and has moved to `lokiCanary.tenant` in 12.0.0.

**Fix:** Move `tenant.name` and `tenant.password` to the `lokiCanary.tenant` section.

### Index gateway CrashLoopBackOff

**Cause:** The old `indexGateway.persistence.inMemory` field is no longer recognized.

**Fix:** Replace `inMemory: true` with the equivalent `dataVolumeParameters` configuration as described in [9.x to 10.0](#9x-to-100-index-gateway-persistence-restructured).

### Unexpected deployment mode change

**Cause:** The default `deploymentMode` changed from `SimpleScalable` to `Monolithic`. If you relied on the default without explicitly setting it, your deployment mode changed on upgrade.

**Fix:** Explicitly set `deploymentMode: SimpleScalable` in your values file (or choose `Monolithic` or `Distributed`) and re-run the upgrade.

### Unknown field errors from enterprise values

**Cause:** The `enterprise.*` section was removed in 8.0.0. Any leftover keys cause Helm to reject the values.

**Fix:** Remove the entire `enterprise` section from your values file.

## Further reading

- [Community chart changelog](https://grafana-community.github.io/helm-charts/changelog/?chart=loki)
- [Community chart README - Upgrading](https://github.com/grafana-community/helm-charts/blob/main/charts/loki/README.md#upgrading)
- [Helm chart 6.x upgrade guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-to-6x/)
