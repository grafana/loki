# fluent-plugin-loki

[Fluentd](https://fluentd.org/) output plugin to ship logs to a Loki server.

TODO: write description for you plugin.

## Installation

### RubyGems

```
$ gem install fluent-plugin-loki
```

### Bundler

Add following line to your Gemfile:

```ruby
gem "fluent-plugin-loki"
```

And then execute:

```
$ bundle
```

## Configuration

You can generate configuration template:

```
$ fluent-plugin-config-format output loki
```

You can copy and paste generated documents here.

## Development

After checking out the repo, run `bin/setup` to install dependencies. Then, run `rake spec` to run the tests. You can also run `bin/console` for an interactive prompt that will allow you to experiment.

To install this gem onto your local machine, run `bundle exec rake install`. To release a new version, update the version number in `version.rb`, and then run `bundle exec rake release`, which will create a git tag for the version, push git commits and tags, and push the `.gem` file to [rubygems.org](https://rubygems.org).

To create the gem: `gem build fluent-plugin-loki.gemspec`

Useful additions:
  `gem install rubocop`

## Copyright

* Copyright(c) 2018- Grafana Labs
* License
  * Apache License, Version 2.0
