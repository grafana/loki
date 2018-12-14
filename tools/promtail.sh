#!/bin/sh

INSTANCEID="${1:-}"
APIKEY="${2:-}"
INSTANCEURL="${3:-}"
NAMESPACE="${4:-default}"

if [ -z "$INSTANCEID" -o -z "$APIKEY" -o -z "$INSTANCEURL" -o -z "$NAMESPACE" ]; then
    echo "usage: $0 <instanceId> <apiKey> <url> <namespace>"
    exit 1
fi

TEMPLATE=$(cat <<'YAML'
apiVersion: v1
data:
  promtail.yml: |
    scrape_configs:
    - job_name: kubernetes-pods
      kubernetes_sd_configs:
      - role: pod
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_node_name
        target_label: __host__
      - action: drop
        regex: ^$
        source_labels:
        - __meta_kubernetes_pod_label_name
      - action: replace
        replacement: $1
        separator: /
        source_labels:
        - __meta_kubernetes_namespace
        - __meta_kubernetes_pod_label_name
        target_label: job
      - action: replace
        source_labels:
        - __meta_kubernetes_namespace
        target_label: namespace
      - action: replace
        source_labels:
        - __meta_kubernetes_pod_name
        target_label: instance
      - replacement: /var/log/pods/$1
        separator: /
        source_labels:
        - __meta_kubernetes_pod_uid
        - __meta_kubernetes_pod_container_name
        target_label: __path__
    - job_name: kubernetes-pods-app
      kubernetes_sd_configs:
      - role: pod
      relabel_configs:
      - source_labels:
        - __meta_kubernetes_pod_node_name
        target_label: __host__
      - action: drop
        regex: ^$
        source_labels:
        - __meta_kubernetes_pod_label_app
      - action: replace
        replacement: $1
        separator: /
        source_labels:
        - __meta_kubernetes_namespace
        - __meta_kubernetes_pod_label_app
        target_label: job
      - action: replace
        source_labels:
        - __meta_kubernetes_namespace
        target_label: namespace
      - action: replace
        source_labels:
        - __meta_kubernetes_pod_name
        target_label: instance
      - action: labelmap
        regex: __meta_kubernetes_pod_label_(.+)
      - replacement: /var/log/pods/$1
        separator: /
        source_labels:
        - __meta_kubernetes_pod_uid
        - __meta_kubernetes_pod_container_name
        target_label: __path__
kind: ConfigMap
metadata:
  name: promtail
---
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: promtail
spec:
  minReadySeconds: 10
  template:
    metadata:
      labels:
        name: promtail
    spec:
      containers:
      - args:
        - -client.url=https://<instanceId>:<apiKey>@<instanceUrl>/api/prom/push
        - -config.file=/etc/promtail/promtail.yml
        env:
        - name: HOSTNAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        image: grafana/promtail:master
        imagePullPolicy: IfNotPresent
        name: promtail
        ports:
        - containerPort: 80
          name: http-metrics
        securityContext:
          privileged: true
          runAsUser: 0
        volumeMounts:
        - mountPath: /etc/promtail
          name: promtail
        - mountPath: /var/log
          name: varlog
        - mountPath: /var/lib/docker/containers
          name: varlibdockercontainers
          readOnly: true
      serviceAccount: promtail
      tolerations:
      - effect: NoSchedule
        operator: Exists
      volumes:
      - configMap:
          name: promtail
        name: promtail
      - hostPath:
          path: /var/log
        name: varlog
      - hostPath:
          path: /var/lib/docker/containers
        name: varlibdockercontainers
  updateStrategy:
    type: RollingUpdate
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: promtail
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRole
metadata:
  name: promtail
rules:
- apiGroups:
  - ""
  resources:
  - nodes
  - nodes/proxy
  - services
  - endpoints
  - pods
  verbs:
  - get
  - list
  - watch
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRoleBinding
metadata:
  name: promtail
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: promtail
subjects:
- kind: ServiceAccount
  name: promtail
  namespace: <namespace>
YAML
)

echo "$TEMPLATE" | sed \
  -e "s#<instanceId>#${INSTANCEID}#" \
  -e "s#<apiKey>#${APIKEY}#" \
  -e "s#<instanceUrl>#${INSTANCEURL}#" \
  -e "s#<namespace>#${NAMESPACE}#"
