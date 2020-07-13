# -*- encoding: utf-8 -*-
# stub: puma 4.3.5 java lib

Gem::Specification.new do |s|
  s.name = "puma".freeze
  s.version = "4.3.5"
  s.platform = "java".freeze

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "changelog_uri" => "https://github.com/puma/puma/blob/master/History.md", "msys2_mingw_dependencies" => "openssl" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["Evan Phoenix".freeze]
  s.date = "2020-05-19"
  s.description = "Puma is a simple, fast, threaded, and highly concurrent HTTP 1.1 server for Ruby/Rack applications. Puma is intended for use in both development and production environments. It's great for highly concurrent Ruby implementations such as Rubinius and JRuby as well as as providing process worker support to support CRuby well.".freeze
  s.email = ["evan@phx.io".freeze]
  s.executables = ["puma".freeze, "pumactl".freeze]
  s.files = ["bin/puma".freeze, "bin/pumactl".freeze]
  s.homepage = "http://puma.io".freeze
  s.licenses = ["BSD-3-Clause".freeze]
  s.required_ruby_version = Gem::Requirement.new(">= 2.2".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Puma is a simple, fast, threaded, and highly concurrent HTTP 1.1 server for Ruby/Rack applications".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<nio4r>.freeze, ["~> 2.0"])
    else
      s.add_dependency(%q<nio4r>.freeze, ["~> 2.0"])
    end
  else
    s.add_dependency(%q<nio4r>.freeze, ["~> 2.0"])
  end
end
