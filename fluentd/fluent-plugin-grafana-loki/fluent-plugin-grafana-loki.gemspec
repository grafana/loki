# frozen_string_literal: true

$LOAD_PATH.push File.expand_path('lib', __dir__)

Gem::Specification.new do |spec|
  spec.name    = 'fluent-plugin-grafana-loki'
  spec.version = '1.1.0'
  spec.authors = %w[woodsaj briangann]
  spec.email   = ['awoods@grafana.com', 'brian@grafana.com']

  spec.summary       = 'Output plugin to ship logs to a Grafana Loki server'
  spec.description   = 'Output plugin to ship logs to a Grafana Loki server'
  spec.homepage      = 'https://github.com/grafana/loki/'
  spec.license       = 'Apache-2.0'

  # test_files, files  = `git ls-files -z`.split("\x0").partition do |f|
  #   f.match(%r{^(test|spec|features)/})
  # end
  spec.files         = Dir.glob('{bin,lib}/**/*') + %w[LICENSE README.md]
  spec.executables   = spec.files.grep(%r{^exe/}) { |f| File.basename(f) }
  spec.require_paths = ['lib']

  # spec.files         = files
  # spec.executables   = files.grep(%r{^bin/}) { |f| File.basename(f) }
  # spec.test_files    = test_files
  # spec.require_paths = ['lib']

  spec.add_development_dependency 'bundler', '~> 1.15'
  spec.add_development_dependency 'rake', '~> 12.0'
  spec.add_development_dependency 'rspec', '~> 3.0'
  spec.add_runtime_dependency 'fluentd', ['>= 0.14.10', '< 2']
end
