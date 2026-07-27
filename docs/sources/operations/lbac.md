---
title: Use label-based access control with Loki
menuTitle: Access control
description: Describes how to use label-based access control to only query logs that meet specific label requirements.
aliases:
weight:
---

# Use label-based access control with Loki

{{< admonition type="note" >}}
This feature is experimental and available from v3.7.3. For the latest releases, refer to the [Release notes](https://grafana.com/docs/loki/<LOKI_VERSION>/release-notes/).
{{< /admonition >}}

Label-based access control (LBAC) restricts the logs a tenant can query to those that match one or more [Prometheus label selectors](https://prometheus.io/docs/prometheus/latest/querying/basics/#time-series-selectors). You associate a set of selectors with a tenant, and queries from that tenant only return data from log streams that match at least one of the selectors. Because the selectors are combined with OR, this corresponds to [disjunctive normal form](https://en.wikipedia.org/wiki/Disjunctive_normal_form), which lets you express any required policy.

LBAC builds on Loki's multi-tenancy: policies are scoped per tenant, so you must run Loki in multi-tenant mode. For more information, refer to the [multi-tenancy](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/) documentation.

## Security model

Loki does not include its own authentication layer. Both the tenant (`X-Scope-OrgID`) and the label policy (`X-Prom-Label-Policy`) are read from trusted HTTP headers on incoming requests.

{{< admonition type="warning" >}}
Never expose the Loki API directly to clients. A client that can reach Loki can set the `X-Scope-OrgID` and `X-Prom-Label-Policy` headers itself and bypass label-based access control entirely. You must run an authentication gateway in front of Loki that authenticates the request, sets the tenant, and attaches the correct label policy.
{{< /admonition >}}

The gateway is responsible for stripping any user supplied `X-Scope-OrgID` and `X-Prom-Label-Policy` and replacing them with values it derives from the authenticated identity. One open-source gateway that sets these headers is [db-auth-gateway](https://github.com/grafana/db-auth-gateway/). For general guidance on putting an authenticating reverse proxy in front of Loki, refer to the [authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/) documentation.

## Enable label-based access control

LBAC requires multi-tenant mode, which is the default. Ensure authentication is enabled so that requests carry a tenant:

```yaml
auth_enabled: true
```

Then enable LBAC:

```yaml
lbac:
  enabled: true
```

This can also be set with the `-lbac.enabled` command-line flag. When enabled, Loki parses the `X-Prom-Label-Policy` header on incoming requests and enforces the policies it contains.

## Setting up a label policy

A label policy is conveyed to Loki in the `X-Prom-Label-Policy` request header. This header is set by your authentication gateway, not by clients. Each header value has the form:

```
<tenant>:<URL-encoded selector>
```

The selector is a standard label matcher set such as `{env="dev"}`, URL-encoded so that it is safe to carry in a header. For example, the policy `{env="dev"}` for tenant `tenant1` is encoded as:

```
X-Prom-Label-Policy: tenant1:%7Benv%3D%22dev%22%7D
```

To associate multiple selectors with a tenant, set multiple values. You can either repeat the header or separate the encoded policies with a comma. The selectors are combined with OR: a stream is returned if it matches any one of them.

## Exclude a label

One common use case for an LBAC policy is to exclude logs that have a specific label. For example, to exclude all log lines with the label `secret=true`, use a selector with `secret!="true"`:

```
{secret!="true"}
```

## Use multiple selectors

To allow access to both the production and development environments while excluding logs with the label `secret=true` in the production environment, use multiple selectors:

```
{secret!="true", env="prod"}
{env="dev"}
```

These selectors enforce the policy as follows:

* `{secret!="true", env="prod"}` matches and returns log lines from the production environment that do not have the `secret: true` label.
* `{env="dev"}` matches and returns log lines from the development environment, even if they have the `secret: true` label.

## Label policy scope

Label policies are only applied to the log stream selector of the Loki query. They do not apply to label filter expressions. For more information, refer to the [Log stream selector](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#log-stream-selector) and [Label filter expression](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/#label-filter-expression) sections in the documentation about [Log queries](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/).

## Writing log lines

Label-based access control is not enforced on write (push) requests. A tenant that is allowed to write can push log lines with any labels, regardless of the label policy that applies to its queries.

## Alertmanager and ruler

Label policies are not enforced by the Alertmanager and ruler. This means that the requests they serve contain everything for a particular tenant without applying label-based access control. For example, listing all rule groups in the ruler returns all rule groups for the tenant, even if a label selector in the policy would exclude some of the labels on the rules.
