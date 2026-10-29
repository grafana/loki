# Legacy data object fixtures

The `legacy-*.dataobj` files are data objects in the format from before
streams sections recorded a stream layout. Their logs sections record the
layout in their own sort info. LogMerge and compactor tests use them to check
that such objects still compact.

They were written by `logsobj.Builder.FlushAndSort` at commit `ce961feb09`
(`refactor(dataobj): Builder owns sorting of new objects (#25498)`). Each holds
tenant `T` and two records per stream:

| File | Sort schema | Streams (`app`, `cluster`) |
|---|---|---|
| `legacy-cluster-a.dataobj` | `label:cluster` | `a`/`c-a`, `b`/`c-b` |
| `legacy-cluster-b.dataobj` | `label:cluster` | `a`/`c-a`, `c`/`c-c` |
| `legacy-app.dataobj` | `label:app` | `a`/`c-a` |

To regenerate them, check out that commit and run a test in
`pkg/dataobj/logsobj` that appends those streams with `tenantOverrides{"T":
schema}`, calls `FlushAndSort`, and writes the object bytes to these files.
The fixtures exist only to test the legacy layout fallback in
`logs.LegacyLayoutOf`.
