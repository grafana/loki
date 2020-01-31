## Kubernetes Query Frontend Example

### Disclaimer

This aims to be a general purpose example; there are a number of substitutions to make for it to work correctly. These variables take the form of <variable_name>. You should override them with specifics to your environment.

### Use case

It's a common occurrence to start running Loki as a single binary while trying it out in order to simplify deployments and defer learning the (initially unnecessary) nitty gritty details. As we become more comfortable with its paradigms and begin migrating towards a more production ready deployment there are a number of things to be aware of. A common bottleneck is on the read path: queries that executed effortlessly on small data sets may churn to a halt on larger ones. Sometimes we can solve this with more queriers. However, that doesn't help when our queries are too large for a single querier to execute. Then we need the query frontend.

#### Parallelization

One of the most important functions of the query frontend is the ability to split larger queries into smaller ones, execute them in parallel, and stitch the results back together. How often it splits them is determined by the `querier.split-queries-by-interval` flag or the yaml config `queryrange.split_queriers_by_interval`. With this set to `1h`, the frontend will dissect a day long query into 24 one hour queries, distribute them to the queriers, and collect the results. This is immensely helpful in production environments as it not only allows us to perform larger queries via aggregation, but also evens the work distribution across queriers so that one or two are not stuck with impossibly large queries while others are left idle.

### Configuration

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
  log_queries_longer_than: 5s
  downstream: querier.<namespace>.svc.cluster.local:9091
  compress_responses: true
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
  - name: query-frontend-http
    port: 9091
    protocol: TCP
    targetPort: 9091
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
        - containerPort: 9091
          name: http
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
