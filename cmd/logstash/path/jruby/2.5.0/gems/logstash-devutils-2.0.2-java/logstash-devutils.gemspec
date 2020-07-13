raise "Only JRuby is supported" if RUBY_PLATFORM != "java"

Gem::Specification.new do |spec|
  files = %x{git ls-files}.split("\n")

  spec.name = "logstash-devutils"
  spec.version = "2.0.2"
  spec.license = "Apache-2.0"
  spec.authors = ["Elastic"]
  spec.email = "info@elastic.co"
  spec.homepage = "http://www.elastic.co/guide/en/logstash/current/index.html"

  spec.summary = %q{An assortment of tooling/libraries to make Logstash plugin development and releasing a bit easier.}
  spec.description = %q{logstash-devutils gem is meant to be used as a development dependency from other plugins/gems.}

  spec.files = files
  spec.require_paths << "lib"

  spec.platform = "java"

  spec.required_ruby_version = '>= 2.3'

  # Please note that devutils is meant to be used as a development dependency from other plugins/gems.
  # As such, devutils' OWN development dependencies (any add_development_dependency) will be ignored when
  # bundling a plugin/gem which uses/depends on devutils.
  #
  # This is why all devutils own dependencies should normally be specified as add_runtime_dependency.

  # It is important to specify rspec "~> 3.0" and not "~> 3.0.0" (or any other version to the patch level)
  # otherwise the version constrain will have to be met up to the minor release version and only allow
  # patch level differences. In other words, "~> 3.0.0" will allow 3.0.1 but not 3.1.
  # This comment has been added because this has caused weird dependencies issues since we
  # update rspec quite freely upon new minor releases.
  spec.add_runtime_dependency "rspec", "~> 3.0" # MIT License
  spec.add_runtime_dependency "rspec-wait" # MIT License

  spec.add_runtime_dependency "rake" # MIT License
  spec.add_runtime_dependency "gem_publisher" # MIT License
  spec.add_runtime_dependency "minitar" # GPL2|Ruby License

  spec.add_runtime_dependency "logstash-core", ">= 6.3"

  # Some plugins are (still) using insist by their own, but we no longer force this dependency on others.
  #spec.add_runtime_dependency "insist" # (Apache 2.0 license)
  spec.add_runtime_dependency "kramdown", '1.14.0'
  spec.add_runtime_dependency "stud", " >= 0.0.20"
  spec.add_runtime_dependency "fivemat"
  spec.add_runtime_dependency "logstash-codec-plain"
end
