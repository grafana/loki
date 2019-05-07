# Promtail Setups

## Design Documentation

* [Extracting labels from logs](./design/labels.md)

## Daemonset method

Daemonset will deploy promtail on every node within the kubernetes cluster.

Daemonset deployment is great to collect all of the container logs within the
cluster. It is great solution for single tenant.  All of the logs will send to a
single Loki server.

### Example
```yaml
---Daemonset.yaml
apiVersion: extensions/v1beta1
kind: Daemonset
metadata:
  name: promtail-daemonset
  ...
spec:
  ...
  template:
    spec:
      serviceAccount: SERVICE_ACCOUNT
      serviceAccountName: SERVICE_ACCOUNT
      volumes:
      - name: logs
        hostPath: HOST_PATH
      - name: promtail-config
        configMap
          name: promtail-configmap
      containers:
      - name: promtail-container
         args:
         - -config.file=/etc/promtail/promtail.yaml
         volumeMounts:
         - name: logs
            mountPath: MOUNT_PATH
         - name: promtail-config
            mountPath: /etc/promtail
  ...

---configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: promtail-config
  ...
data:
  promtail.yaml: YOUR CONFIG

---Clusterrole.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: promtail-clusterole
rules:
  - apiGroups:
     resources:
     - nodes
     - services
     - pod
    verbs:
    - get
    - watch
    - list
---ServiceAccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promtail-serviceaccount

---Rolebinding
apiVersion: rbac.authorization.k9s.io/v1
kind: ClusterRoleBinding
metadata:
  name: promtail-clusterrolebinding
subjects:
    - kind: ServiceAccount
       name: promtail-serviceaccount
roleRef:
    kind: ClusterRole
    name: promtail-clusterrole
    apiGroup: rbac.authorization.k8s.io
```

## Sidecar Method

Sidecar method will deploy promtail as a container within a pod that
developer/devops create.

This method will deploy promtail as a sidecar container within a pod.
In a multi-tenant environment, this enables teams to aggregate logs
for specific pods and deployments for example for all pods in a namespace.

### Example
```yaml
---Deployment.yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: my_test_app
  ...
spec: 
  ...
  template:
    spec:
      serviceAccount: SERVICE_ACCOUNT
      serviceAccountName: SERVICE_ACCOUNT
      volumes:
      - name: logs
        hostPath: HOST_PATH
      - name: promtail-config
        configMap
          name: promtail-configmap
      containers:
      - name: promtail-container
         args:
         - -config.file=/etc/promtail/promtail.yaml
         volumeMounts:
         - name: logs
            mountPath: MOUNT_PATH
         - name: promtail-config
            mountPath: /etc/promtail
      ...
  ...

```

### Custom Log Paths

Sometime application create customized log files.  To collect those logs, you
would need to have a customized `__path__` in your scrape_config.

Right now, the best way to watch and tail custom log path is define log filepath
as a label for the pod.

#### Example
```yaml
---Deployment.yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
    name: test-app-deployment
    namespace: your_namespace
    labels:
      logFileName: my_app_log
...

---promtail_config.yaml
...
scrape_configs:
   ...
   - job_name: job_name
      kubernetes_sd_config:
      - role: pod
      relabel_config:
      ...
      - action: replace
        target_label: __path__
        source_labes:
        - __meta_kubernetes_pod_label_logFileName
        replacement: /your_log_file_dir/$1.log
      ...
```
