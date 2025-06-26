# Fluentd output plugin

[Fluentd](https://fluentd.org/) is a data collector for unified logging layer, it can be configured with the Loki output plugin, provided in this folder, to ship logs to Loki.

See the [Fluentd documentation](../../../docs/sources/send-data/fluentd/_index.md) for detailed information.

## Development

After checking out the repo, run `bin/setup` to install dependencies. Then, run `bin/test` to run the tests. You can also run `bin/console` for an interactive prompt that will allow you to experiment.

To install this gem onto your local machine, run `ruby -S bundle exec rake install`. To release a new version, update the version number in `fluent-plugin-grafana-loki.gemspec`, and then run `ruby -S bundle exec rake release`, which will create a git tag for the version, push git commits and tags, and push the `.gem` file to [rubygems.org](https://rubygems.org).

To create the gem: `ruby -S gem build fluent-plugin-grafana-loki.gemspec`

Useful additions:

```bash
ruby -S gem install rubocop
```

## Testing

You can test out your changes using the `docker-compose` setup found in the `docker` directory, simply start things up using the following command:

```bash
cd docker
docker compose up
```

This will start up instances of `loki` (storing logs), `fluentd` (shipping logs to loki), and `fluent-bit` (tailing `/varlog/syslog` and sending that to loki via fluentd).

Give things about 20 seconds to settle then run the following command in a separate terminal to verify logs are being stored in Loki:

```bash
curl "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={job="fluentd"}' --data-urlencode 'step=300' | jq .data.result
```

The expected output is something like this:

```json
[
  {
    "stream": {
      "job": "fluentd"
    },
    "values": [
      [
        "1751021592790139275",
        "log=\"Jun 27 11:53:12 a-2wbuz1yvo5lhz dockerd: time=\\\"2025-06-27T11:53:12.789500251+01:00\\\" level=debug msg=\\\"Name To resolve: loki.\\\"\""
      ],
      ... lots more log entries
    ]
  }
]
```

To send custom log messages directly to fluentd, issue a command like so:

```bash
curl -v -X POST -H "Content-Type: application/json" -d "{ \"log\": \"hello there\" }" http://localhost:8080/loki.output
```

If you're finding it hard to test your custom messages this way (and see the resulting log output), you can stop the fluent-bit container:

```bash
docker compose stop fluent-bit
```

**NOTE:** Code changes are not reflected live, you'll need to restart the fluentd container in order for them to take effect:

```bash
docker compose restart fluentd
```

## Build and publish gem

To build and publish a gem to
[rubygems](https://rubygems.org/gems/fluent-plugin-grafana-loki) you first need
to update the version in the `fluent-plugin-grafana-loki.gemspec` file.
Then update the `VERSION` variable in the `Makefile` to match the new version number.
Create a PR with the changes against the `main` branch und run `make
fluentd-plugin-push` from the root of the project once the PR has been merged.

## Copyright

- Copyright(c) 2018- Grafana Labs
- License
  - Apache License, Version 2.0
