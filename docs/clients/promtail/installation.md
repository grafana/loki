# Installing Promtail

Promtail is distributed as a [binary](#binary), [Docker container](#docker), and
[Helm chart](#helm).

## Binary

Every release includes binaries for Promtail which can be found on the
[Releases page](https://github.com/grafana/loki/releases).

## Docker

```bash
# modify tag to most recent version
$ docker pull grafana/promtail:v1.0.0
```

## Helm

Make sure that Helm is
[installed](https://helm.sh/docs/using_helm/#installing-helm) and
[deployed](https://helm.sh/docs/using_helm/#installing-tiller) to your cluster.
Then you can add Loki's chart repository to Helm:

```bash
$ helm repo add loki https://grafana.github.io/loki/charts
```

And the chart repository can be updated by running:

```bash
$ helm repo update
```

Finally, Promtail can be deployed with:

```bash
$ helm upgrade --install promtail loki/promtail --set "loki.serviceName=loki"
```

## Kubernetes

### DaemonSet (Recommended)

A `DaemonSet` will deploy `promtail` on every node within a Kubernetes cluster.

The DaemonSet deployment is great to collect the logs of all containers within a
cluster. It's the best solution for a single-tenant model.

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

### Sidecar

The Sidecar method deploys `promtail` as a sidecar container for a specific pod.
In a multi-tenant environment, this enables teams to aggregate logs for specific
pods and deployments.

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
