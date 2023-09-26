# Loki Nomad examples

## Requirements

### Hard requirements

- recent version of Nomad [installed](https://www.nomadproject.io/docs/install) with healthy Docker driver
- [Consul integration](https://www.nomadproject.io/docs/integrations/consul-integration)
  is enabled in Nomad
- access to S3 storage

### Optional requirements

- [Vault integration](https://www.nomadproject.io/docs/integrations/vault-integration)
  for providing S3 credentials securely
- Traefik configured to use
  [Consul provider](https://doc.traefik.io/traefik/providers/consul-catalog/) to
  loadbalance between Loki instances

### Production use

For use in production it is recommended to:

- secure HTTP endpoints with
  [Consul Connect](https://www.nomadproject.io/docs/integrations/consul-connect)
- setup authentication - can be achieved with
  [Traefik](https://doc.traefik.io/traefik/middlewares/http/basicauth/)
- secure GRPC communication with mTLS - can be achived with Vault's
  [PKI secret engine](https://www.vaultproject.io/docs/secrets/pki)

See [loki-distributed](./loki-distributed) README for more info.

## Service discovery when scaling

When using multiple Loki instances memberlist advertises wrong address (see this
[issue](https://github.com/grafana/loki/issues/5610)), that is why these
examples are using Consul ring for service discovery.

Is you are using Nomad then you are probably also using Consul, so this
shouldn't be an issue.

## Run Loki behind loadbalancer

When running multiple instances of Loki incoming requests should be
loadbalanced.

Register Loki in Traefik:

```hcl
tags = [
  "traefik.enable=true",
  "traefik.http.routers.loki.entrypoints=https",
  "traefik.http.routers.loki.rule=Host(`loki.service.consul`)",
]
```

## Setup basicauth

Generate basicauth credentials:

```shell
> docker run --rm httpd:alpine htpasswd -nb promtail password123
promtail:$apr1$Lr55BanK$BV/rE2POaOolkFz8kIfY4/
```

Register Loki in Traefik:

```hcl
tags = [
  "traefik.enable=true",
  "traefik.http.routers.loki.entrypoints=https",
  "traefik.http.routers.loki.rule=Host(`loki.service.consul`)",
  "traefik.http.middlewares.loki.basicauth.users=promtail:$apr1$Lr55BanK$BV/rE2POaOolkFz8kIfY4/",
  "traefik.http.routers.loki.middlewares=loki@consulcatalog",
]
```

Update Promtail config:

```yaml
clients:
  - url: https://loki.service.consul/loki/api/v1/push
    basic_auth:
      username: promtail
      password_file: password123
```

## Use HashiCorp Vault to provide S3 credentials

To provide static credentials:

```hcl
template {
  data = <<-EOH
  {{ with secret "secret/minio/loki" }}
  S3_ACCESS_KEY_ID={{ .Data.data.access_key }}
  S3_SECRET_ACCESS_KEY={{ .Data.data.secret_key }}
  {{- end }}
  EOH

  destination = "secrets/s3.env"
  env         = true
}
```

Is is better to provide dynamic credentials using
[AWS secret engine](https://www.vaultproject.io/docs/secrets/aws) when using AWS
S3:

```hcl
template {
  data = <<-EOH
  {{ with secret "aws/creds/loki" -}}
  S3_ACCESS_KEY_ID={{ .Data.access_key }}
  S3_SECRET_ACCESS_KEY={{ .Data.secret_key }}
  {{- end }}
  EOH

  destination = "secrets/s3.env"
  env         = true
}
```

## Supply alerting rules to Loki ruler with `local` ruler storage

### Using [`artifact` stanza](https://www.nomadproject.io/docs/job-specification/artifact)

Alert rules can be downloaded from remote storage using artifact stanza. It
supports:

- Git
- Mercurial
- HTTP
- Amazon S3
- Google GCP

Example with git:

```hcl
artifact {
  source      = "git::github.com/<someorg>/<repo>//<subdirectory_with_loki_rules>"
  destination = "local/rules/"
}
```

### Using local files

Alert rules can be stored locally (beside job definition) and provided to Loki
ruler container with
[`template`](https://www.nomadproject.io/docs/job-specification/template) stanza
and some HCL magic, namely:

- [fileset](https://www.nomadproject.io/docs/job-specification/hcl2/functions/file/fileset) -
  to generate a list of files
- [file](https://www.nomadproject.io/docs/job-specification/hcl2/functions/file/file) -
  to get the content of a file
- [dynamic](https://www.nomadproject.io/docs/job-specification/hcl2/expressions#dynamic-blocks) -
  to dynamically generate `template` stanza for each file found

Example:

```shell
> tree rules/
rules/
└── fake
    └── some-alerts.yml

1 directory, 1 file
```

```hcl
dynamic "template" {
  for_each = fileset(".", "rules/**")

  content {
    data            = file(template.value)
    destination     = "local/${template.value}"
    left_delimiter  = "[["
    right_delimiter = "]]"
  }
}
```

Each file will end up in `/local/rules/` inside ruler container.

### Using Consul K/V and Terraform

```shell
> tree loki-rules/
loki-rules/
└── fake
    └── some-alerts.yml

1 directory, 1 file
```

Using Terraform
[Consul provider](https://registry.terraform.io/providers/hashicorp/consul/latest/docs/resources/keys)
put all files in `loki-rules/` to Consul K/V

```hcl
resource "consul_keys" "loki-rules" {
  dynamic "key" {
    for_each = fileset("${path.module}/loki-rules", "**")
    content {
      path   = "configs/loki-rules/${trimsuffix(key.value, ".yml")}"
      value  = file("${file.module}/loki-rules/${key.value}")
      delete = true
    }
  }
}
```

Provide rules from K/V to Loki ruler container inside Nomad with
[`safeTree`](https://github.com/hashicorp/consul-template/blob/main/docs/templating-language.md#safetree)

```hcl
template {
  data = <<-EOF
  {{- range safeTree "configs/loki-rules" }}
  ---
  {{ .Value | indent 2 }}
  {{ end -}}
  EOF

  destination   = "local/rules/fake/rules.yml"
  change_mode   = "signal"
  change_signal = "SIGINT"
}
```

When updated in Consul K/V rules will be automatically updated in Loki ruler.
