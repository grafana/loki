**What this PR does / why we need it**:

**Which issue(s) this PR fixes**:
Fixes #<issue number>

**Special notes for your reviewer**:

**Checklist**
- [ ] Reviewed the [`CONTRIBUTING.md`](https://github.com/grafana/loki/blob/main/CONTRIBUTING.md) guide (**required**)
- [ ] Documentation added
- [ ] Tests updated
- [ ] Title matches the required conventional commits format, see [here](https://www.conventionalcommits.org/en/v1.0.0/)
  - **Note** that Promtail is considered to be feature complete, and future development for logs collection will be in [Grafana Alloy](https://github.com/grafana/alloy). As such, `feat` PRs are unlikely to be accepted unless a case can be made for the feature actually being a bug fix to existing behavior.
- [ ] Changes that require user attention or interaction to upgrade are documented in `docs/sources/setup/upgrade/_index.md`
- [ ] For Helm chart changes bump the Helm chart version in `production/helm/loki/Chart.yaml` and update `production/helm/loki/CHANGELOG.md` and `production/helm/loki/README.md`. [Example PR](https://github.com/grafana/loki/commit/d10549e3ece02120974929894ee333d07755d213)
- [ ] If the change is deprecating or removing a configuration option, update the `deprecated-config.yaml` and `deleted-config.yaml` files respectively in the `tools/deprecated-config-checker` directory. [Example PR](https://github.com/grafana/loki/pull/10840/commits/0d4416a4b03739583349934b96f272fb4f685d15)
