local k = import 'ksonnet-util/kausal.libsonnet';

{
  _config+:: {
    htpasswd_contents: error 'must specify htpasswd contents',

    // This is inserted into the gateway Nginx config file
    // under the server directive
    gateway_server_snippet: '',
    gateway_tenant_id: '1',
  },

  _images+:: {
    nginx: 'nginx:1.15.1-alpine',
  },

  local secret = k.core.v1.secret,

  gateway_secret:
    secret.new('gateway-secret', {
      '.htpasswd': std.base64($._config.htpasswd_contents),
    }),

  local configMap = k.core.v1.configMap,

  gateway_config:
    configMap.new('gateway-config') +
    configMap.withData({
      'nginx.conf': |||
        worker_processes  5;  ## Default: 1
        error_log  /dev/stderr;
        pid        /tmp/nginx.pid;
        worker_rlimit_nofile 8192;

        events {
          worker_connections  4096;  ## Default: 1024
        }

        http {
          default_type application/octet-stream;
          log_format   main '$remote_addr - $remote_user [$time_local]  $status '
            '"$request" $body_bytes_sent "$http_referer" '
            '"$http_user_agent" "$http_x_forwarded_for"';
          access_log   /dev/stderr  main;
          sendfile     on;
          tcp_nopush   on;
          resolver %(dns_resolver)s;

          server {
            listen               80;
            auth_basic           "Prometheus";
            auth_basic_user_file /etc/nginx/secrets/.htpasswd;
            proxy_set_header     X-Scope-OrgID %(gateway_tenant_id)s;

            location = /api/prom/push {
              proxy_pass       http://distributor.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
            }

            location = /api/prom/tail {
              proxy_pass       http://querier.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
              proxy_set_header Upgrade $http_upgrade;
              proxy_set_header Connection "upgrade";
            }

            location ~ /api/prom/.* {
              proxy_pass       http://query-frontend.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
            }

            location = /loki/api/v1/push {
              proxy_pass       http://distributor.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
            }

            location = /loki/api/v1/tail {
              proxy_pass       http://querier.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
              proxy_set_header Upgrade $http_upgrade;
              proxy_set_header Connection "upgrade";
            }

            location ~ /loki/api/.* {
              proxy_pass       http://query-frontend.%(namespace)s.svc.cluster.local:%(http_listen_port)s$request_uri;
            }

            %(gateway_server_snippet)s
          }
        }
      ||| % $._config,
    }),

  local container = k.core.v1.container,
  local containerPort = k.core.v1.containerPort,

  gateway_container::
    container.new('nginx', $._images.nginx) +
    container.withPorts(k.core.v1.containerPort.new(name='http', port=80)) +
    k.util.resourcesRequests('50m', '100Mi'),

  local deployment = k.apps.v1.deployment,

  gateway_deployment:
    deployment.new('gateway', 3, [
      $.gateway_container,
    ]) +
    deployment.mixin.spec.template.metadata.withAnnotationsMixin({
      config_hash: std.md5(std.toString($.gateway_config)),
    }) +
    k.util.configVolumeMount('gateway-config', '/etc/nginx') +
    k.util.secretVolumeMount('gateway-secret', '/etc/nginx/secrets', defaultMode=420) +
    k.util.antiAffinity +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxSurge(5) +
    deployment.mixin.spec.strategy.rollingUpdate.withMaxUnavailable(1),

  gateway_service:
    k.util.serviceFor($.gateway_deployment, $._config.service_ignored_labels),
}
