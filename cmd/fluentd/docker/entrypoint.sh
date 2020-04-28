#!/usr/bin/dumb-init /bin/sh

exec fluentd -c ${FLUENTD_CONF} -p /fluentd/plugins --gemfile /fluentd/Gemfile ${FLUENTD_OPT}