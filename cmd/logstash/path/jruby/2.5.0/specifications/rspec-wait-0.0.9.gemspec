# -*- encoding: utf-8 -*-
# stub: rspec-wait 0.0.9 ruby lib

Gem::Specification.new do |s|
  s.name = "rspec-wait".freeze
  s.version = "0.0.9"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["Steve Richert".freeze]
  s.date = "2016-07-11"
  s.description = "Wait for conditions in RSpec".freeze
  s.email = "steve.richert@gmail.com".freeze
  s.homepage = "https://github.com/laserlemon/rspec-wait".freeze
  s.licenses = ["MIT".freeze]
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Wait for conditions in RSpec".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<rspec>.freeze, [">= 3", "< 4"])
      s.add_development_dependency(%q<bundler>.freeze, ["~> 1.12"])
      s.add_development_dependency(%q<rake>.freeze, ["~> 11.2"])
    else
      s.add_dependency(%q<rspec>.freeze, [">= 3", "< 4"])
      s.add_dependency(%q<bundler>.freeze, ["~> 1.12"])
      s.add_dependency(%q<rake>.freeze, ["~> 11.2"])
    end
  else
    s.add_dependency(%q<rspec>.freeze, [">= 3", "< 4"])
    s.add_dependency(%q<bundler>.freeze, ["~> 1.12"])
    s.add_dependency(%q<rake>.freeze, ["~> 11.2"])
  end
end
