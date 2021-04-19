# Win event log

This is a fork of https://github.com/influxdata/telegraf/tree/master/plugins/inputs/win_eventlog to re-use most of the syscall implementation for the eventlog since Telegraf accumulator pattern is bit different from Prometheus/Loki model. 

We also only modify how subscription restart, we continue work where we left it off using a saved file model.

