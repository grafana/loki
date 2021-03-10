package ingester

import (
	"text/template"

	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
)

func Deployment() {
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeEmptyDirInMemory()
	fs.AddFile()
	k.Run(fs, "")
}


var deployment = template.Must(template.New("ingester-deployment.yaml").Parse(deploymentTemplate))

const deploymentTemplate = `
--- 
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/component: ingester
    app.kubernetes.io/name: loki
  name: loki-ingester
spec:
  replicas: ${{LOKI_INGESTER_REPLICAS}}
  selector:
    matchLabels:
      app.kubernetes.io/component: ingester
      app.kubernetes.io/instance: observatorium
      app.kubernetes.io/name: loki
      app.kubernetes.io/part-of: observatorium
      loki.grafana.com/gossip: "true"
  serviceName: observatorium-loki-ingester-grpc
  template:
    metadata:
      labels:
        app.kubernetes.io/component: ingester
        app.kubernetes.io/instance: observatorium
        app.kubernetes.io/name: loki
        app.kubernetes.io/part-of: observatorium
        app.kubernetes.io/tracing: jaeger-agent
        loki.grafana.com/gossip: "true"
    spec:
      containers:
      - args:
        - -target=ingester
        - -config.file=/etc/loki/config/config.yaml
        - -limits.per-user-override-config=/etc/loki/config/overrides.yaml
        - -log.level=error
        - -distributor.replication-factor=${LOKI_REPLICATION_FACTOR}
        - -querier.worker-parallelism=${LOKI_QUERY_PARALLELISM}
        image: ${LOKI_IMAGE}:${LOKI_IMAGE_TAG}
        livenessProbe:
          failureThreshold: 10
          httpGet:
            path: /metrics
            port: 3100
            scheme: HTTP
          periodSeconds: 30
        name: observatorium-loki-ingester
        ports:
        - containerPort: 3100
          name: metrics
        - containerPort: 9095
          name: grpc
        - containerPort: 7946
          name: gossip-ring
        readinessProbe:
          httpGet:
            path: /ready
            port: 3100
            scheme: HTTP
          initialDelaySeconds: 15
          timeoutSeconds: 1
        resources:
          limits:
            cpu: ${LOKI_INGESTER_CPU_LIMITS}
            memory: ${LOKI_INGESTER_MEMORY_LIMITS}
          requests:
            cpu: ${LOKI_INGESTER_CPU_REQUESTS}
            memory: ${LOKI_INGESTER_MEMORY_REQUESTS}
        volumeMounts:
        - mountPath: /etc/loki/config/
          name: config
          readOnly: false
        - mountPath: /data
          name: storage
          readOnly: false
      - args:
        - --reporter.grpc.host-port=dns:///jaeger-collector-headless.${JAEGER_COLLECTOR_NAMESPACE}.svc:14250
        - --reporter.type=grpc
        - --jaeger.tags=pod.namespace=$(NAMESPACE),pod.name=$(POD)
        env:
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        image: ${JAEGER_AGENT_IMAGE}:${JAEGER_AGENT_IMAGE_TAG}
        livenessProbe:
          failureThreshold: 5
          httpGet:
            path: /
            port: 14271
            scheme: HTTP
        name: jaeger-agent
        ports:
        - containerPort: 5778
          name: configs
        - containerPort: 6831
          name: jaeger-thrift
        - containerPort: 14271
          name: metrics
        resources:
          limits:
            cpu: 128m
            memory: 128Mi
          requests:
            cpu: 32m
            memory: 64Mi
      serviceAccountName: ${SERVICE_ACCOUNT_NAME}
      volumes:
      - configMap:
          name: observatorium-loki
        name: config
  volumeClaimTemplates:
  - metadata:
      labels:
        app.kubernetes.io/instance: observatorium
        app.kubernetes.io/name: loki
        app.kubernetes.io/part-of: observatorium
      name: storage
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: ${LOKI_PVC_REQUEST}
      storageClassName: ${STORAGE_CLASS}
`