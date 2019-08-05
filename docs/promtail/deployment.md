# Installation
Promtail is distributed in binary and in container form.

Once it is installed, you have basically two options for operating it:
Either as a daemon sitting on every node, or as a sidecar for the application.

This usually only depends on the configuration though.
## Binary
Every release includes binaries:

```bash
# download a binary (adapt app, os and arch as needed)
# installs v0.2.0. Go to the releases page for up to date URLs
$ curl -fSL -o "/usr/local/bin/promtail.gz" "https://github.com/grafana/promtail/releases/download/v0.2.0/promtail-linux-amd64.gz"
$ gunzip "/usr/local/bin/promtail.gz"

# make sure it is executable
$ chmod a+x "/usr/local/bin/promtail"
```

Binaries for macOS and Windows are also provided at the [releases page](https://github.com/grafana/loki/releases).

## Docker
```bash
# adapt tag to most recent version
$ docker pull grafana/promtail:v0.2.0
```

## Kubernetes
On Kubernetes, you will use the Docker container above. However, you have too
choose whether you want to run in daemon mode (`DaemonSet`) or sidecar mode
(`Pod container`) in before.
### Daemonset method (Recommended)

A `DaemonSet` will deploy `promtail` on every node within the Kubernetes cluster.

This deployment is great to collect the logs of all containers within the
cluster. It is the best solution for a single tenant. 

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
  name: promtail-clusterrole
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

### Sidecar Method
This method will deploy `promtail` as a sidecar container within a pod.
In a multi-tenant environment, this enables teams to aggregate logs
for specific pods and deployments for example for all pods in a namespace.

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
