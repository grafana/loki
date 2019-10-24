{
  _config+:: {
    htpasswd_contents: error 'must specify htpasswd contents',
  },

  _images+:: {
    nginx: 'nginx:1.15.1-alpine',
  },

  local secret = $.core.v1.secret,

  gateway_secret:
    secret.new('gateway-secret', {
      '.htpasswd': std.base64($._config.htpasswd_contents),
    }),

  local configMap = $.core.v1.configMap,

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
          resolver kube-dns.kube-system.svc.cluster.local;

          server {
            listen               80;
            auth_basic           “Prometheus”;
            auth_basic_user_file /etc/nginx/secrets/.htpasswd;
            proxy_set_header     X-Scope-OrgID 1;

            location = /api/prom/push {
              proxy_pass      http://distributor.%(namespace)s.svc.cluster.local$request_uri;
            }

            location ~ /api/prom/.* {
              proxy_pass      http://querier.%(namespace)s.svc.cluster.local$request_uri;
            }

            location = /loki/api/v1/push {
              proxy_pass      http://distributor.%(namespace)s.svc.cluster.local$request_uri;
            }

            location ~ /loki/api/.* {
              proxy_pass      http://querier.%(namespace)s.svc.cluster.local$request_uri;
            }
          }
        }
      ||| % $._config,
    }),

  local container = $.core.v1.container,
  local containerPort = $.core.v1.containerPort,

  gateway_container::
    container.new('nginx', $._images.nginx) +
    container.withPorts($.core.v1.containerPort.new('http', 80)) +
    $.util.resourcesRequests('50m', '100Mi'),

  local deployment = $.apps.v1beta1.deployment,

  gateway_deployment:
    deployment.new('gateway', 3, [
      $.gateway_container,
    ]) +
    $.util.configVolumeMount('gateway-config', '/etc/nginx') +
    $.util.secretVolumeMount('gateway-secret', '/etc/nginx/secrets', defaultMode=420) +
    $.util.antiAffinity,

  gateway_service:
    $.util.serviceFor($.gateway_deployment),
}
