# Fluentd output plugin

[Fluentd](https://fluentd.org/) is a data collector for unified logging layer, it can be configured with the Loki output plugin, provided in this folder, to ship logs to Loki.

See [docs/sources/fluentd/README.md](../../../docs/sources/send-data/fluentd/_index.md) for detailed information.

## Development

After checking out the repo, run `bin/setup` to install dependencies. Then, run `bin/test` to run the tests. You can also run `bin/console` for an interactive prompt that will allow you to experiment.

To install this gem onto your local machine, run `ruby -S bundle exec rake install`. To release a new version, update the version number in `fluent-plugin-grafana-loki.gemspec`, and then run `ruby -S bundle exec rake release`, which will create a git tag for the version, push git commits and tags, and push the `.gem` file to [rubygems.org](https://rubygems.org).

To create the gem: `ruby -S gem build fluent-plugin-grafana-loki.gemspec`

Useful additions:

```bash
ruby -S gem install rubocop
```

## Testing

Start Loki using:

```bash
docker run -it -p 3100:3100 grafana/loki:latest
```

Verify that Loki accept and stores logs:

```bash
curl -H "Content-Type: application/json" -XPOST -s "http://localhost:3100/loki/api/v1/push" --data-raw "{\"streams\": [{\"stream\": {\"job\": \"test\"}, \"values\": [[\"$(date +%s)000000000\", \"fizzbuzz\"]]}]}"
curl "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={job="test"}' --data-urlencode 'step=300' | jq .data.result
```

The expected output is:

```json
[
  {
    "stream": {
      "job": "test"
    },
    "values": [
      [
        "1588337198000000000",
        "fizzbuzz"
      ]
    ]
  }
]
```

Start and send test logs with Fluentd using:

```bash
LOKI_URL=http://{{ IP }}:3100 make fluentd-test
```

Verify that syslogs are being feeded into Loki:

```bash
curl "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={job="fluentd"}' --data-urlencode 'step=300' | jq .data.result
```

The expected output is:

```json
[
  {
    "stream": {
      "job": "fluentd"
    },
    "values": [
      [
        "1588336950379591919",
        "log=\"May  1 14:42:30 ibuprofen avahi-daemon[859]: New relevant interface vethb503225.IPv6 for mDNS.\""
      ],
      ...
    ]
  }
]
```

## Build and publish gem

To build and publish a gem to
[rubygems](https://rubygems.org/gems/fluent-plugin-grafana-loki) you first need
to update the version in the `fluent-plugin-grafana-loki.gemspec` file.
Then update the `VERSION` variable in the `Makefile` to match the new version number.
Create a PR with the changes against the `main` branch und run `make
fluentd-plugin-push` from the root of the project once the PR has been merged.

## Copyright

* Copyright(c) 2018- Grafana Labs
* License
  * Apache License, Version 2.0
