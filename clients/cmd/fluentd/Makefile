.PHONY: clean

SHELL    = /usr/bin/env bash -o pipefail
# needs to match the name in fluent-plugin-grafana-loki.gemspec
NAME    := fluent-plugin-grafana-loki
# needs to match the version in fluent-plugin-grafana-loki.gemspec
VERSION := 1.2.20

clean:
	rm -f $(NAME)-*.gem
	rm -rf .bundle

.bundle:
	ruby --version
	@echo ""
	ruby -S gem install bundler --version 2.3.4
	ruby -S bundle config silence_root_warning true
	ruby -S bundle config set --local path ./vendor/bundle

fluentd-plugin: .bundle
	ruby -S bundle install --gemfile=Gemfile

$(NAME)-$(VERSION).gem: .bundle
	ruby -S gem build $(NAME).gemspec

fluentd-plugin-push: $(NAME)-$(VERSION).gem
	ruby -S gem push $(NAME)-$(VERSION).gem
