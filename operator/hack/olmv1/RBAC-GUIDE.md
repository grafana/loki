# OLM v1 RBAC Requirements Guide

This guide explains the RBAC permissions required for the installer ServiceAccount and how to troubleshoot permission errors.

## Why OLM v1 Requires a ServiceAccount

**OLM v0** runs with cluster-admin permissions by default - insecure but simple.

**OLM v1** follows a **least-privilege security model**:
- You must provide a ServiceAccount with explicit permissions
- OLM v1 uses this ServiceAccount to install the operator
- If permissions are missing, installation fails with clear error messages

## Required Permissions Explained

The installer ServiceAccount needs permissions to install operator components:

### 1. CRD Management
```yaml
- apiGroups: ["apiextensions.k8s.io"]
  resources: ["customresourcedefinitions"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Install LokiStack, AlertingRule, RecordingRule, RulerConfig CRDs

### 2. RBAC Management
```yaml
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["clusterroles", "clusterrolebindings", "roles", "rolebindings"]
  verbs: ["create", "get", "list", "update", "patch", "escalate", "bind", "watch"]
```

Create operator's ClusterRole and bindings
Only grant to trusted ServiceAccounts in controlled namespaces

### 3. ServiceAccount Management
```yaml
- apiGroups: [""]
  resources: ["serviceaccounts"]
  verbs: ["create", "get", "list", "update", "patch", "delete", "watch"]

- apiGroups: [""]
  resources: ["serviceaccounts/finalizers"]
  verbs: ["update"]
```

Create operator ServiceAccount and manage owner references
Required for proper resource lifecycle management

### 4. Deployment Management
```yaml
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["create", "get", "list", "update", "patch", "delete", "watch"]
```

Create and manage operator Deployment

### 5. Service Management
```yaml
- apiGroups: [""]
  resources: ["services"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Create webhook Service and metrics Service

### 6. ConfigMap Management
```yaml
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Create operator configuration ConfigMaps

### 7. Secret Management
```yaml
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Manage operator secrets (metrics tokens, webhook certs)

### 8. Webhook Configuration
```yaml
- apiGroups: ["admissionregistration.k8s.io"]
  resources: ["validatingwebhookconfigurations", "mutatingwebhookconfigurations"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Register validation and mutation webhooks

### 9. Cert-Manager Integration (Optional)
```yaml
- apiGroups: ["cert-manager.io"]
  resources: ["certificates", "issuers"]
  verbs: ["create", "get", "list", "update", "patch"]
```

Support built-in certificate management if enabled

### 10. Namespace Access
```yaml
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
```

Verify installation namespace exists

### 11. Prometheus Operator Resources
```yaml
- apiGroups: ["monitoring.coreos.com"]
  resources: ["servicemonitors", "prometheusrules"]
  verbs: ["create", "get", "list", "update", "patch", "watch"]
```

Create ServiceMonitors and PrometheusRules if monitoring feature gates enabled

### 12. ClusterExtension Resources
```yaml
- apiGroups: ["olm.operatorframework.io"]
  resources: ["clusterextensions"]
  verbs: ["get", "list"]

- apiGroups: ["olm.operatorframework.io"]
  resources: ["clusterextensions/finalizers"]
  verbs: ["update"]
```

Read ClusterExtension for owner references and manage finalizers

---

## Troubleshooting Permission Errors

**If you encounter permission errors:**

1. Check the error message for the specific resource and verb
2. Add that permission to `serviceaccount-installer.yaml`
3. Apply the updated ServiceAccount
4. Delete and recreate the ClusterExtension

**Example:**
```
User "system:serviceaccount:openshift-operators-redhat:loki-operator-installer" 
cannot get resource "secrets" in namespace "openshift-operators-redhat"
```

**Solution:**
1. Identify missing resource (e.g., "secrets")
2. Add permission to `serviceaccount-installer.yaml`:
   ```yaml
   - apiGroups: [""]  # or appropriate API group
     resources: ["secrets"]
     verbs: ["create", "get", "list", "update", "patch", "watch"]
   ```
3. Apply updated ServiceAccount:
   ```bash
   kubectl apply -f hack/olmv1/serviceaccount-installer.yaml
   ```
4. Delete and recreate ClusterExtension:
   ```bash
   kubectl delete clusterextension loki-operator
   kubectl apply -f hack/olmv1/clusterextension-loki-operator.yaml
   ```

---

## Testing Your ServiceAccount

**Before deploying the operator, verify the ServiceAccount has required permissions:**

```bash
# Test CRD permission
kubectl auth can-i create customresourcedefinitions \
  --as=system:serviceaccount:openshift-operators-redhat:loki-operator-installer

# Test RBAC permission
kubectl auth can-i create clusterroles \
  --as=system:serviceaccount:openshift-operators-redhat:loki-operator-installer

# Test with escalate/bind (should be "yes")
kubectl auth can-i escalate clusterroles \
  --as=system:serviceaccount:openshift-operators-redhat:loki-operator-installer
```

**All should return `yes` if permissions are correct.**

---

## Operator Upgrades and RBAC

**Critical:** If a new operator version requires additional permissions, the upgrade will fail.

### Upgrade Workflow When Permissions Change

1. **Check release notes** for RBAC changes
2. **Update ServiceAccount** with new permissions:
   ```bash
   # Edit serviceaccount-installer.yaml to add new permissions
   kubectl apply -f hack/olmv1/serviceaccount-installer.yaml
   ```
3. **Then upgrade operator**:
   ```bash
   kubectl patch clusterextension loki-operator --type=merge \
     -p '{"spec":{"source":{"catalog":{"version":"X.Y.Z"}}}}'
   ```

**Without updating the ServiceAccount first, the upgrade will fail with permission errors.**

---

## Minimal Permission Set

**The permissions in `serviceaccount-installer.yaml` are the minimum required set** discovered through testing on OCP 4.22.0-rc.5.

**Do NOT remove permissions** unless you've verified they're unused through testing.

**Do NOT add permissions** unless you encounter specific errors requiring them.

