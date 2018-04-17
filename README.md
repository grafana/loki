# Logish: Like Prometheus, but for logs.

[![CircleCI](https://circleci.com/gh/grafana/logish/tree/master.svg?style=svg&circle-token=618193e5787b2951c1ea3352ad5f254f4f52313d)](https://circleci.com/gh/grafana/logish/tree/master) [Design doc](https://docs.google.com/document/d/11tjK_lvp1-SVsFZjgOTr1vV3-q6vBAsZYIQ5ZeYBkyM/edit)

Logish is a horizontally-scalable, highly-available, multi-tenant, log aggregation
system inspired by Prometheus.  It is design to be very cost effective, as it does
not index the contents of the logs, but rather a set of labels for each log steam.
