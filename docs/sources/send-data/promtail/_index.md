---
title: Promtail agent
menuTitle:  Promtail
description: How to use the Promtail agent to ship logs to Loki
aliases: 
- ../clients/promtail/ # /docs/loki/latest/clients/promtail/
weight:  300
---
# Promtail agent

{{< admonition type="warning" >}}
Promtail is end of life (EOL) as of March 2, 2026.  Commercial support has ended. No future support or updates will be provided. All future feature development will occur in Grafana Alloy.  
Note that the deprecation of Promtail does NOT include the lambda-promtail client.
{{< /admonition >}}

If you are currently using Promtail, you must migrate to Alloy or another supported client.

The Alloy migration documentation includes a migration tool for converting your Promtail configuration to an Alloy configuration with a single command.

## Resources

- Links to [Migrate to Alloy documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/setup/migrate/migrate-to-alloy/).
- [Medium post with migration instructions](https://medium.com/@syed_usman_ahmed/docker-users-who-use-grafana-loki-with-promtail-need-to-migrate-to-grafana-alloy-as-promtail-the-a20a) written by a Grafana Developer Advocate.
- [Video for Docker users with Promtail migration guide](https://www.youtube.com/watch?v=3W99Go4S39E) filmed by a Grafana Developer Advocate.
