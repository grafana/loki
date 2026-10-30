**What this PR does / why we need it**:

**Which issue(s) this PR fixes**:

Fixes #<issue number>

**Special notes for your reviewer**:

**Checklist**:

- [ ] Reviewed the [`CONTRIBUTING.md`](https://github.com/grafana/loki/blob/main/CONTRIBUTING.md) guide (**required**)
- [ ] Documentation added
- [ ] Tests updated
- [ ] The PR title follows [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) and the description start with a capital letter
  - User-facing change types (`feat`, `fix`, `perf`, `revert`) will be added to the changelog
  - Hidden change types (`chore`, `docs`, `ci`, `refactor`, `test`, etc.) will not
  - Breaking changes (`!` before the colon, or a `BREAKING CHANGE:` footer) will be added whatever the type
- [ ] Changes that require user attention or interaction to upgrade are documented in `docs/sources/setup/upgrade/_index.md`
- [ ] If the change is deprecating or removing a configuration option, update the `deprecated-config.yaml` and `deleted-config.yaml` files respectively in the `tools/deprecated-config-checker` directory. [Example PR](https://github.com/grafana/loki/pull/10840/commits/0d4416a4b03739583349934b96f272fb4f685d15)
- [Backporting](https://github.com/grafana/loki/blob/main/docs/sources/community/maintaining/release/backport-commits.md). Checking these will not trigger a backport but can be used for verification that the backporting is complete
  - [ ] Backport to `release-4.0.0`
  - [ ] Backport to `release-3.7.x`
  - [ ] Backport to `release-3.6.x`
  - [ ] Backport to latest `k` release (dev)
  - [ ] Backport to previous `k` release (prod)