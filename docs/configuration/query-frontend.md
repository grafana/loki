## Disclaimer

This aims to be a general purpose example. There are a number of substitutions to make for these to work correctly. These variables take the form of <variable_name>. Override them with specifics to your environment.

## Configuration

Use this shared config file to get the benefits of query parallelisation and caching with the query-frontend component. In addition to this configuration, start the querier and query-frontend components with `-target=querier` and `-target=query-frontend`, respectively.

```yaml
# Disable the requirement that every request to Cortex has a
# X-Scope-OrgID header. `fake` will be substituted in instead.
auth_enabled: false

# We don't want the usual /api/prom prefix.
http_prefix:

server:
  http_listen_port: 9091

query_range:
  # make queries more cache-able by aligning them with their step intervals
  align_queries_with_step: true
  max_retries: 5
  # parallelize queries in 15min intervals
  split_queries_by_interval: 15m 
  cache_results: true

  results_cache:
    max_freshness: 10m
    cache:
      # We're going to use the in-process "FIFO" cache, but you can enable
      # memcached below.
      enable_fifocache: true
      fifocache:
        size: 1024
        validity: 24h

      # If you want to use a memcached cluster, configure a headless service
      # in Kubernetes and Loki will discover the individual instances using
      # a SRV DNS query.  Loki will then do client-side hashing to spread
      # the load evenly.
      # memcached:
      #   memcached_client:
      #     host: memcached.default.svc.cluster.local
      #     service: memcached
      #     consistent_hash: true

frontend:
  # 256 length tenant queues per frontend
  max_outstanding_per_tenant: 256
  log_queries_longer_than: 5s
  compress_responses: true
  
frontend_worker:
  address: query-frontend.<namespace>.svc.cluster.local:9095
  grpc_client_config:
    max_send_msg_size: 1.048576e+08
  parallelism: 8
```


## Kubernetes Deployment

Here's an example k8s deployment which can consume the above configuration yaml. Note that the above configuration snippet should be merged with the rest of your configuration and exposed as the `loki` configmap.

### Frontend Service
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
  labels:
    name: query-frontend
  name: query-frontend
  namespace: <namespace>
spec:
  ports:
  - name: query-frontend-http-metrics
    port: 80
    protocol: TCP
    targetPort: 80
  - name: query-frontend-grpc
    port: 9095
    protocol: TCP
    targetPort: 9095
  selector:
    name: query-frontend
  sessionAffinity: None
  type: ClusterIP
```

### Frontend Deployment

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  labels:
    name: query-frontend
  name: query-frontend
  namespace: <namespace>
spec:
  minReadySeconds: 10
  replicas: 2
  selector:
    matchLabels:
      name: query-frontend
  template:
    metadata:
      labels:
        name: query-frontend
    spec:
      containers:
      - args:
        - -config.file=/etc/loki/config.yaml
        - -log.level=debug
        - -target=query-frontend
        image: grafana/loki:<version>
        imagePullPolicy: IfNotPresent
        name: query-frontend
        ports:
        - containerPort: 80
          name: http-metrics
          protocol: TCP
        - containerPort: 9095
          name: grpc
          protocol: TCP
        resources:
          limits:
            memory: 1200Mi
          requests:
            cpu: "2"
            memory: 600Mi
        volumeMounts:
        - mountPath: /etc/loki
          name: loki
      restartPolicy: Always
      terminationGracePeriodSeconds: 30
      volumes:
      - configMap:
          defaultMode: 420
          name: loki
        name: loki
```
