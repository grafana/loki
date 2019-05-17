# Promtail Setups

## Daemonset method

Daemonset will deploy `promtail` on every node within the Kubernetes cluster.

Daemonset deployment is great to collect all of the container logs within the
cluster. It is great solution for single tenant.  All of the logs will send to a
single Loki server.

Check the `production` folder for examples of a daemonset deployment for kubernetes using both helm and ksonnet.

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

## Sidecar Method

Sidecar method will deploy `promtail` as a container within a pod that
developer/devops create.

This method will deploy `promtail` as a sidecar container within a pod.
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

Right now, the best way to watch and tail custom log path is define log file path
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

### Custom Client options

`promtail` client configuration uses the [Prometheus http client](https://godoc.org/github.com/prometheus/common/config) implementation.
Therefore you can configure the following authentication parameters in the `client` or `clients` section.

```yaml
---promtail_config.yaml
...

# Simple client
client:
  [ <client_option> ]

# Multiple clients
clients:
  [ - <client_option> ]
...
```

>Note: Passing the `-client.url` from command line is only valid if you set the `client` section.

#### `<client_option>`

```yaml
  # Sets the `url` of loki api push endpoint
  url: http[s]://<host>:<port>/api/prom/push

  # Sets the `Authorization` header on every promtail request with the
  # configured username and password.
  # password and password_file are mutually exclusive.
  basic_auth:
    username: <string>
    password: <secret>
    password_file: <string>

  # Sets the `Authorization` header on every promtail request with
  # the configured bearer token. It is mutually exclusive with `bearer_token_file`.
  bearer_token: <secret>

  # Sets the `Authorization` header on every promtail request with the bearer token
  # read from the configured file. It is mutually exclusive with `bearer_token`.
  bearer_token_file: /path/to/bearer/token/file

  # Configures the promtail request's TLS settings.
  tls_config:
    # CA certificate to validate API server certificate with.
    # If not provided Trusted CA from sytem will be used.
    ca_file: <filename>

    # Certificate and key files for client cert authentication to the server.
    cert_file: <filename>
    key_file: <filename>

    # ServerName extension to indicate the name of the server.
    # https://tools.ietf.org/html/rfc4366#section-3.1
    server_name: <string>

    # Disable validation of the server certificate.
    insecure_skip_verify: <boolean>

  # Optional proxy URL.
  proxy_url: <string>

  # Maximum wait period before sending batch
  batchwait: 1s

  # Maximum batch size to accrue before sending, unit is byte
  batchsize: 102400

  # Maximum time to wait for server to respond to a request
  timeout: 10s

  backoff_config:
    # Initial backoff time between retries
    minbackoff: 100ms
    # Maximum backoff time between retries
    maxbackoff: 5s
    # Maximum number of retires when sending batches, 0 means infinite retries
    maxretries: 5

  # The labels to add to any time series or alerts when communicating with loki
  external_labels: {}
```
