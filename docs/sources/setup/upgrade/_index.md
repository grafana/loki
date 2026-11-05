---
title: Upgrade Loki
menuTitle:  Upgrade
description: Upgrading Grafana Loki
aliases:
  - ../upgrading/
weight: 400
---

# Upgrade Loki

Every attempt is made to keep Grafana Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes we are forced to make decisions between ease of use and ease of maintenance.

If we have any expectation of difficulty upgrading, we will document it here.

As more versions are released it becomes more likely unexpected problems arise moving between multiple versions at once.
If possible try to stay current and do sequential updates. If you want to skip versions, try it in a development environment before attempting to upgrade production.

## Checking for config changes

<!-- vale Grafana.Spelling = NO -->

Using docker you can check changes between 2 versions of Loki with a command like this:

```bash
export OLD_LOKI=2.9.4
export NEW_LOKI=3.0.0
export CONFIG_FILE=local-config.yaml
diff --color=always --side-by-side <(docker run --rm -t -v "${PWD}":/config grafana/loki:${OLD_LOKI} -config.file=/etc/loki/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') <(docker run --rm -t -v "${PWD}":/config grafana/loki:${NEW_LOKI} -config.file=/etc/loki/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') | less -R
```

The `tr -d '\r'` is likely not necessary for most people, seems like WSL2 was sneaking in some windows newline characters...

The output is incredibly verbose as it shows the entire internal config struct used to run Loki, you can play around with the diff command if you prefer to only show changes or a different style output.

## Main / Unreleased

### Loki

#### Statistics in detected-field query responses

The `/loki/api/v1/detected_fields` and `/loki/api/v1/detected_field/{name}/values` JSON
responses now include a top-level `stats` object containing the underlying log query's
statistics. Existing `fields`, `values`, and `limit` fields retain their format.
Clients that validate response fields strictly must allow the new `stats` field.

#### `ip()` label filters no longer keep lines with an `__error__` label

An `ip()` label filter used to keep every log line that already carried an `__error__` label, without checking the address. It now compares the line like a numeric, duration, or bytes filter does. A line that failed in an earlier stage, such as `json`, usually has no value for the label, so the filter drops it. For example, `| json | addr = ip("10.0.0.0/8")` no longer returns the lines that failed to parse, and a metric query with this filter no longer fails on them. To keep those lines, combine the filter with an `__error__` filter, for example `| addr = ip("10.0.0.0/8") or __error__ != ""`.

## Further reading

To upgrade to one of the versions of **Loki 4.x**, refer to [Upgrade Loki 4.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-4.x/).

To upgrade to one of the versions of **Loki 3.x**, refer to [Upgrade Loki 3.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-3.x/).

To upgrade to one of the versions of **Loki 2.x**, refer to [Upgrade Loki 2.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-2.x/).

To upgrade to one of the versions of **Loki 1.x**, refer to [Upgrade Loki 1.x versions](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/upgrade/upgrade-1.x/).
