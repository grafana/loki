---
headless: true
---

# Storage

Unlike other logging systems, %% product %% is built around the idea of only indexing
metadata about your logs: labels (just like Prometheus labels). Log data itself
is then compressed and stored in chunks in object stores such as S3 or GCS, or
even locally on the filesystem. A small index and highly compressed chunks
simplifies the operation and significantly lowers the cost of %% product %%.

Until %% product %% 2.0, index data was stored in a separate index.

%% product %% 2.0 brings an index mechanism named 'boltdb-shipper' and is what we now call Single Store %% product %%.
This index type only requires one store, the object store, for both the index and chunks.
More detailed information can be found on the [operations page]({{< relref "../operations/storage/boltdb-shipper.md" >}}).

Some more storage details can also be found in the [operations section]({{< relref "../operations/storage/_index.md" >}}).
