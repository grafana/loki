# pkg/logline/config/

The `logline` section of Loki's config file, declared apart from the code that reads it.

## Why this package exists

`pkg/loki` embeds this section into `loki.Config`. The obvious home for the types would
be `pkg/logline/queryfrontend`, next to the middleware they configure, but that package
imports `pkg/loki` to read the assembled Loki config, so `pkg/loki` importing it back
would close a cycle. Splitting the declarations out breaks it: this package depends only
on `pkg/logline/store`.

`pkg/logline/queryfrontend` re-exports `Config`, `MiddlewareConfig` and
`ShardPlanningConfig` as type aliases, so middleware code still reads in terms of its own
package.

## Invariants

1. **Never import `pkg/loki` here.** That is the cycle this package exists to avoid.
   Fields populated from the surrounding Loki config (`QueryIngestersWithin`,
   `QuerySplitDuration`) are filled in by the caller at assembly time, not read from
   `loki.Config` here.

2. **Flags stay in the `logline` namespace.** `-logline.*`, `-logline-store.*`,
   `-logline-query-frontend.*`. `MiddlewareConfig` deliberately has no unprefixed
   `RegisterFlags`: its natural prefix is `query-frontend`, which is Loki's own namespace,
   and registering there would panic the binary on a duplicate flag.

3. **`Config.Validate` is a no-op unless `Enabled`.** `store.Config.Validate` requires
   `min_date`, which has no sensible default, so validating unconditionally would reject
   every config that does not use logline.

4. **`Validate` applies defaults as a side effect.** It must run before the config is
   used. `ShardPlanningConfig` is the awkward case: its `UnmarshalYAML` only runs when the
   `shard_planning` key is present, so a config that omits the key leaves the struct zero
   until `Validate` (or `ApplyDefaults`) fills it.
