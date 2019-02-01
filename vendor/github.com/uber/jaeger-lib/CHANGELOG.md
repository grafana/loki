Changes by Version
==================

1.5.0 (2018-05-11)
------------------

- Change default metrics namespace separator from colon to underscore (#43) <Juraci Paixão Kröhling>
- Use an interface to be compatible with Prometheus 0.9.x (#42) <Pavel Nikolov>


1.4.0 (2018-03-05)
------------------

- Reimplement expvar metrics to be tolerant of duplicates (#40)


1.3.1 (2018-01-12)
-------------------

- Add Gopkg.toml to allow using the lib with `dep`


1.3.0 (2018-01-08)
------------------

- Move rate limiter from client to jaeger-lib [#35](https://github.com/jaegertracing/jaeger-lib/pull/35)


1.2.1 (2017-11-14)
------------------

- *breaking* Change prometheus.New() to accept options instead of fixed arguments


1.2.0 (2017-11-12)
------------------

- Support Prometheus metrics directly [#29](https://github.com/jaegertracing/jaeger-lib/pull/29).


1.1.0 (2017-09-10)
------------------

- Re-releasing the project under Apache 2.0 license.


1.0.0 (2017-08-22)
------------------

- First semver release.
