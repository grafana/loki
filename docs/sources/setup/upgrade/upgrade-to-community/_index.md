---
title: Upgrade from the Loki Helm chart to the Community Helm chart
menuTitle: Upgrade to Community Helm chart
description: Upgrade from the Loki repository Helm chart (6.x) to the Grafana Community Helm chart (13.x).
weight: 900
keywords:
  - upgrade
  - community
  - helm
---

# Upgrade from the Loki Helm chart to the Community Helm chart

The Loki Helm chart has moved from the [Loki repository](https://github.com/grafana/loki) to the [Grafana Community Helm Charts repository](https://github.com/grafana-community/helm-charts). Chart version 6.55.0 (appVersion 3.6.7) was the last release from the Loki repository. Chart version 13.1.2 (appVersion 3.7.1) is the current release from the community repository at the time this topic was published.

This guide walks you through upgrading from 6.55.0 to 13.x, which spans seven major chart versions (7 through 13), each with breaking changes.

{{< admonition type="warning" >}}
Grafana Enterprise Logs (GEL) support was removed from the community chart in version 8.0.0. If you are a GEL user, **do not migrate** to the community chart. The `grafana/loki` chart remains available for GEL users.
{{< /admonition >}}

{{< admonition type="caution" >}}
This upgrade spans several major versions with accumulated breaking changes. Test the upgrade in a non-production environment before applying it to production.
{{< /admonition >}}

## Prerequisites

Before you begin:

- Kubernetes 1.25 or later is required (enforced since chart version 7.0.0).
- Helm 3.x is required.
- Back up your current Helm release values:

  ```bash
  helm get values <RELEASE_NAME> -o yaml > backup-values.yaml
  ```

- Back up any persistent volumes and object store indexes.

## Update your Helm repository

The chart is now published to a different Helm repository and Open Container Initiative (OCI) registry.  The official Helm documentation now recommends using OCI registries as they implement unified storage, distribution, and improved security.

Old location for 6.x from the Loki repository:

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm upgrade <RELEASE_NAME> grafana/loki -f values.yaml
```

New location for 7.x and later from the community repository:

```bash
helm repo add grafana-community https://grafana-community.github.io/helm-charts
helm repo update
helm upgrade <RELEASE_NAME> grafana-community/loki -f values.yaml --version 13.1.2
```

Or if you are using OCI for 7.x and later:

```bash
helm upgrade <RELEASE_NAME> oci://ghcr.io/grafana-community/helm-charts/loki -f values.yaml --version 13.1.2
```

## Update your values file for breaking changes

The following sections describe every breaking change between chart versions 6.55.0 and 13.x, grouped by the major version that introduced each change. Review each section and update your values file accordingly.

{{< admonition type="caution" >}}
Check the Loki Helm Chart [README](https://github.com/grafana-community/helm-charts/tree/main/charts/loki#upgrading) for the latest changes and upgrade information. The Grafana Community is constantly improving the charts.
{{< /admonition >}}

### 6.x to 7.0: Kubernetes API cleanup

Support for deprecated Kubernetes APIs and PodSecurityPolicy has been removed. Kubernetes 1.25 or later is now required.

Remove the following from your values file:

```yaml
rbac:
  pspEnabled: false        # remove this key
  pspAnnotations: {}       # remove this key
```

The `rbac` section in 13.x only contains:

```yaml
rbac:
  sccEnabled: false
  sccAllowHostDirVolumePlugin: false
  namespaced: false
```

| If you have   | using   | update them to |
| ------------- | ------------- | ------------- |
| custom Ingress resources | `networking.k8s.io/v1beta1` | `networking.k8s.io/v1` |
| PodDisruptionBudget resources | `policy/v1beta1` | `policy/v1` |

### 7.x to 8.0: Enterprise support removed

Enterprise support is removed in the community Helm chart.

Remove the entire `enterprise` configuration section from your chart, including any `enterprise.*` keys:

```yaml
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
```

{{< admonition type="note" >}}
The Helm Chart in the Loki repository is being maintained for GEL customers only.
{{< /admonition >}}

### 8.x to 9.0: Self-monitoring and Grafana Agent Operator removed

The `monitoring.selfMonitoring` section and the `grafana-agent-operator` subchart dependency have been removed.

Remove the `monitoring.selfMonitoring` section from your values file:

```yaml
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

Also remove `monitoring.serviceMonitor.metricsInstance`, if present. It referenced a Grafana Agent CRD that no longer exists.

Loki canary tenant authentication has moved from `monitoring.selfMonitoring.tenant` to the top-level `lokiCanary.tenant` section. If you were using canary tenant auth, update as follows:

Before (6.x):

```yaml
monitoring:
  selfMonitoring:
    tenant:
      name: "self-monitoring"
      password: "my-password"
```

After (13.x):

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

Before (6.x):

```yaml
indexGateway:
  persistence:
    enabled: false
    inMemory: false
    size: 10Gi
```

After (13.x):

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

Remove from your values file if present:

```yaml
read:
  legacyReadTarget: false  # remove this key
```

If you had `legacyReadTarget: true` (running the 2-target read/write mode without a backend), you must now run the 3-target mode (read, write, backend) or switch to a different deployment mode.

### 11.x to 12.0: Deployment mode renamed and default changed

The default `deploymentMode` has changed from `SimpleScalable` to `Monolithic`, and `SingleBinary` has been renamed to `Monolithic`.

Before (6.x default):

```yaml
deploymentMode: SimpleScalable
```

After (13.x default):

```yaml
deploymentMode: Monolithic
```

Action required based on your current mode:

If you are running `SimpleScalable` (the 6.x default), explicitly set it in your values file to preserve your current deployment:

```yaml
deploymentMode: SimpleScalable
```

{{< admonition type="note" >}}
`SimpleScalable` is deprecated and will be removed in Loki 4.0. Plan to migrate to `Monolithic` or `Distributed`.
{{< /admonition >}}

If you are running `SingleBinary`, update the value to the new name:

```yaml
# Before
deploymentMode: SingleBinary

# After
deploymentMode: Monolithic
```

If you are running `Distributed`, no change is needed.

### 12.x to 13.0: Ephemeral volume persistence flattened

The persistence configuration for ephemeral volumes has been flattened across all components. The nested `persistence.ephemeralDataVolume` structure has been replaced with a `persistence.type` field.

{{< admonition type="note" >}}
If you are migrating directly from 6.55.0 and have never used the `ephemeralDataVolume` configuration introduced in the community chart, this change doesn't require action on your existing values. However, be aware of the new `persistence.type` field when configuring persistence for components like the compactor or bloom-builder.
{{< /admonition >}}

If you adopted the `ephemeralDataVolume` pattern from any intermediate community chart version, update your values as follows:

Before (12.x):

```yaml
<component>:
  persistence:
    ephemeralDataVolume:
      enabled: true
      accessModes:
        - ReadWriteOnce
      size: 10Gi
      storageClass: null
```

After (13.x):

```yaml
<component>:
  persistence:
    enabled: true
    type: ephemeral
    accessModes:
      - ReadWriteOnce
    size: 10Gi
    storageClass: null
```

The `type` field accepts `pvc` (default for most components) or `ephemeral`. Fields that were nested under `ephemeralDataVolume` (`accessModes`, `size`, `storageClass`, `volumeAttributesClassName`, `selector`, `annotations`, `labels`) now sit directly under `persistence`.

## Review additional deprecations

Several per-component fields have been deprecated in favor of unified blocks. These deprecated per-component service fields apply to indexGateway, compactor, and others.

These still work but will be removed in a future release. Consider updating them now:

| Deprecated                      | Use                                |
| ------------------------------- | ------------------------------- -- |
| indexGateway.serviceLabels      | indexGateway.service.labels        |
| indexGateway.serviceAnnotations | indexGateway.service.annotations   |
| indexGateway.serviceType        | indexGateway.service.type          |
| indexGateway.appProtocol        | indexGateway.service.type          |
| indexGateway.maxUnavailable     | podDisruptionBudget.maxUnavailable |

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

## Perform the upgrade

After updating your values file, run the upgrade:

```bash
helm upgrade <RELEASE_NAME> grafana-community/loki \
  -f your-updated-values.yaml \
  --version 13.1.2
```

Or using OCI:

```bash
helm upgrade <RELEASE_NAME> oci://ghcr.io/grafana-community/helm-charts/loki \
  -f your-updated-values.yaml \
  --version 13.1.2
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

## Post-migration cleanup

After a successful upgrade:

- Remove the old Helm repository if no longer needed:

  ```bash
  helm repo remove grafana
  ```

- Update any CI/CD pipelines to reference `grafana-community/loki` or the OCI URL `oci://ghcr.io/grafana-community/helm-charts/loki`.
- Review monitoring dashboards for renamed metrics. Refer to the [Loki 3.x upgrade notes](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/#30) for metric namespace changes.

## Troubleshoot

### Pods fail to start with PodSecurityPolicy errors

**Cause:** Leftover `rbac.pspEnabled` or `rbac.pspAnnotations` in your values file.

**Fix:** Remove both keys from your values file and re-run the upgrade.

### Loki canary fails authentication

**Cause:** Canary tenant configuration was under `monitoring.selfMonitoring.tenant` in 6.x and has moved to `lokiCanary.tenant` in 13.x.

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

### Unknown field `ephemeralDataVolume`

**Cause:** The nested `persistence.ephemeralDataVolume` structure was flattened in 13.0.0. If you adopted this pattern from an intermediate community chart version, the old keys are no longer recognized.

**Fix:** Replace `persistence.ephemeralDataVolume.enabled: true` with `persistence.enabled: true` and `persistence.type: ephemeral`, and move all nested fields directly under `persistence` as described in [12.x to 13.0](#12x-to-130-ephemeral-volume-persistence-flattened).

## Further reading

- [Community chart changelog](https://grafana-community.github.io/helm-charts/changelog/?chart=loki)
- [Community chart README - Upgrading](https://github.com/grafana-community/helm-charts/blob/main/charts/loki/README.md#upgrading)
- [Helm chart 6.x upgrade guide](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-to-6x/)
