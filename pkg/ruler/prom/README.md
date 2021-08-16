This directory was copied from https://github.com/grafana/agent/tree/main/pkg/prom.

We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.

Many changes have been made from the original library.