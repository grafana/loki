# OLM v1 Quick Start Guide

This directory contains files for deploying Loki Operator using OLM v1 instead of OLM v0.

## Prerequisites

### Cluster Requirements

✅ **OpenShift 4.22+** (webhook support GA, enabled by default)

⚠️ **OpenShift 4.20-4.21** (webhook support may need manual enablement)

### Verify OLM v1 Availability

```bash
# Check if OLM v1 CRDs exist
oc get crd clusterextensions.olm.operatorframework.io
oc get crd clustercatalogs.olm.operatorframework.io

# Check for openshift-operator-controller namespace
oc get ns openshift-operator-controller

# Check webhook support (if operator-controller is running)
oc logs -n openshift-operator-controller \
<operator-controller-controller-manager-name> | grep "WebhookProvider"
# --feature-gates=WebhookProviderOpenshiftServiceCA should equal to true

```

---

## Installation

### Step 1: Create Installer ServiceAccount + RBAC

```bash
kubectl apply -f serviceaccount-installer.yaml
```

This creates:
- Namespace: `openshift-operators-redhat`
- ServiceAccount: `loki-operator-installer`
- ClusterRole: `loki-operator-installer` (with required permissions)
- ClusterRoleBinding: `loki-operator-installer`

**Why is this needed?** OLM v1 uses a **least-privilege security model**. Unlike OLM v0 (which runs as cluster-admin), OLM v1 requires you to provide a ServiceAccount with explicit permissions to install operator components.

**Required Permissions Include:**
- CRD management (create, update, watch)
- RBAC management (with escalate/bind verbs)
- Deployment, Service, ConfigMap, Secret management
- Webhook configuration
- Monitoring resources (ServiceMonitors, PrometheusRules)
- Owner reference finalizers

**See [RBAC-GUIDE.md](./RBAC-GUIDE.md) for:**
- Detailed explanation of each permission
- How to troubleshoot permission errors
- What to do when upgrading if permissions change
- Security considerations

### Step 2: Create ClusterExtension

```bash
kubectl apply -f clusterextension-loki-operator.yaml
```

This:
- Installs loki-operator from `openshift-redhat-operators` catalog
- Uses the installer ServiceAccount (least-privilege security model)
- Deploys to `openshift-operators-redhat` namespace

---

## Verification

### Check Installation Status

```bash
# View ClusterExtension
kubectl get clusterextension loki-operator

# Should show:
# NAME            INSTALLED BUNDLE       VERSION   INSTALLED   PROGRESSING
# loki-operator   loki-operator.v6.5.1   6.5.1     True        ...
```

### Check Operator Pod

```bash
kubectl get pods -n openshift-operators-redhat

# Should show:
# NAME                                                READY   STATUS
# loki-operator-controller-manager-XXXXXXXXXX-XXXXX   1/1     Running
```

### Verify CRDs Installed

```bash
kubectl get crd | grep loki.grafana.com

# Should show:
# alertingrules.loki.grafana.com
# lokistacks.loki.grafana.com
# recordingrules.loki.grafana.com
# rulerconfigs.loki.grafana.com
```

### Verify Webhooks Registered

```bash
kubectl get validatingwebhookconfigurations | grep loki

# Should show:
# valertingrule.loki.grafana.com
# vlokistack.loki.grafana.com
# vrecordingrule.loki.grafana.com
# vrulerconfig.loki.grafana.com
```

---

## Upgrading the Operator

### Option 1: Auto-update (if using version range)

OLM v1 automatically updates operators when:
  1. ClusterCatalog polls for new versions (default: every 10 minutes as configured in the openshift-redhat-operators ClusterCatalog CR)
  2. New version matches your version range
  3. Upgrade path exists (if using CatalogProvided policy)

### Option 2: Pin to specific version

Edit the ClusterExtension:
```bash
kubectl patch clusterextension loki-operator --type=merge -p '{
  "spec": {
    "source": {
      "catalog": {
        "version": "6.6.0"
      }
    }
  }
}'
```

### ⚠️ Important: RBAC Changes

If a new operator version requires **additional permissions**, the upgrade will **fail** unless you update the installer ServiceAccount first:

```bash
# 1. Update ServiceAccount with new permissions
kubectl apply -f serviceaccount-installer-updated.yaml

# 2. Then upgrade
kubectl patch clusterextension loki-operator --type=merge -p '...'
```

Always check release notes for RBAC changes before upgrading!

---

## Uninstalling


OLM v1 deletes CRDs and all Custom Resources when you delete the ClusterExtension.

All LokiStack, AlertingRule, RecordingRule, RulerConfig CRs will be deleted.

```bash
# Delete ClusterExtension (deletes operator, CRDs, and all CRs)
kubectl delete clusterextension loki-operator

# Delete installer RBAC
kubectl delete -f serviceaccount-installer.yaml
```

### Manual Cleanup

Operator `ClusterRole` and `ClusterRoleBindings` require require manual cleanup.

---

## Troubleshooting

### Operator not visible in OpenShift Console

**Expected behavior:** OLM v1 operators don't appear in the traditional "Installed Operators" page.

The console UI hasn't been updated for OLM v1 yet. Use:
- `kubectl get clusterextension`
- Search for `ClusterExtension` in console
- Check CRDs to verify installation

### ClusterExtension stuck in Progressing

Check conditions:
```bash
kubectl describe clusterextension loki-operator
```

Common issues:
- **Missing permissions**: Update serviceaccount-installer.yaml
- **Webhook feature gates disabled**: Upgrade to OCP 4.22+ or enable feature gates
- **Catalog not available**: Check `kubectl get clustercatalog`

### Permission errors during installation

The installer ServiceAccount needs extensive permissions. If you see errors like:
```
User "system:serviceaccount:openshift-operators-redhat:loki-operator-installer" cannot get resource "X"
```

Update `serviceaccount-installer.yaml` to include the missing permission, then:
```bash
kubectl apply -f serviceaccount-installer.yaml
kubectl delete clusterextension loki-operator
kubectl apply -f clusterextension-loki-operator-production.yaml
```
