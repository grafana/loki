---
title: About upgrading
weight: 100
---

# About upgrading Grafana Loki

Every attempt is made to keep Grafana Loki backwards compatible, such that upgrades should be low risk and low friction.

Unfortunately Loki is software and software is hard and sometimes we are forced to make decisions between ease of use and ease of maintenance.

If we have any expectation of difficulty upgrading we will document it here.

As more versions are released, it becomes more likely that unexpected problems will arise when moving between multiple versions, skipping intermediate minor versions.
If possible try to stay current and do sequential updates. If you want to skip versions, try it in a development environment before attempting to upgrade in a production environment.

## Checking for configuration changes

Using Docker, you can check changes between two versions of Loki with a command like this:

```
export OLD_LOKI=2.3.0
export NEW_LOKI=2.4.1
export CONFIG_FILE=loki-local-config.yaml
diff --color=always --side-by-side <(docker run --rm -t -v "${PWD}":/config grafana/loki:${OLD_LOKI} -config.file=/config/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') <(docker run --rm -t -v "${PWD}":/config grafana/loki:${NEW_LOKI} -config.file=/config/${CONFIG_FILE} -print-config-stderr 2>&1 | sed '/Starting Loki/q' | tr -d '\r') | less -R
```

The `tr -d '\r'` is likely not necessary. It helps correct for WSL2 sneaking in some Windows newline characters.

The output is incredibly verbose, as it shows the entire internal configuration struct used to run Loki.
Change the `diff` command to suit your needs.

