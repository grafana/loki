# Memcached Jsonnet

A set of extensible configs for running Memcached on Kubernetes.

To use this libary, install [Tanka] and [Jsonnet Bundler].

[Tanka]: https://tanka.dev/
[Jsonnet Bundler]: https://tanka.dev/install#jsonnet-bundler

In your config repo, if you don't have a Tanka application, make a new one (will copy credentials from current context):

```bash
app=your_app_name
mkdir -p $app && cd $app
tk init
```

Then you can install the library with:

```bash
jb install github.com/grafana/jsonnet-libs/memcached
```

Assuming you want to run in the default namespace ('environment' in Tanka parlance), add the following to the file `environments/default/main.jsonnet`:

```jsonnet
local memcached = import "memcached/memcached.libsonnet";

memcached + {
  // Memcached instance used to cache chunks.
  memcached_chunks: $.memcached {
    name: 'memcached',
    max_item_size: '2m',
    memory_limit_mb: 4096,
  },
}
```

Apply your config:

```shell
tk apply default
```

## Customising and Extending

The choice of Tanka for configuring these jobs was intentional; it allows users
to easily override setting in these configurations to suit their needs, without having
to fork or modify this library.  For instance, to override the resource requests
and limits for the Consul container, you would:

```jsonnet
local memcached = import "memcached/memcached.libsonnet";

memcached + {
  // Memcached instance used to cache chunks.
  memcached_chunks: $.memcached {
    name: 'memcached',
    max_item_size: '2m',
    memory_limit_mb: 4096,

    memcached_container+::
     $.util.resourcesRequests("1", "2Gi") +
     $.util.resourcesLimits("2", "4Gi"),
  },
}
```

We sometimes specify config options in a `_config` dict; there are two situations
under which we do this:

- When you must provide a value for the parameter (such as `namespace`).
- When the parameter get referenced in multiple places, and overriding it using
  the technique above would be cumbersome and error prone (such as with `cluster_dns_suffix`).

We use these two guidelines for when to put parameters in `_config` as otherwise
the config field would just become the same as the jobs it declares - and lets
not forget, this whole thing is config - so its completely acceptable to override
pretty much any of it.
