# -*- encoding: utf-8 -*-
# stub: rubyzip 1.3.0 ruby lib

Gem::Specification.new do |s|
  s.name = "rubyzip".freeze
  s.version = "1.3.0"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "bug_tracker_uri" => "https://github.com/rubyzip/rubyzip/issues", "changelog_uri" => "https://github.com/rubyzip/rubyzip/blob/v1.3.0/Changelog.md", "documentation_uri" => "https://www.rubydoc.info/gems/rubyzip/1.3.0", "source_code_uri" => "https://github.com/rubyzip/rubyzip/tree/v1.3.0", "wiki_uri" => "https://github.com/rubyzip/rubyzip/wiki" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["Alexander Simonov".freeze]
  s.date = "2019-09-25"
  s.email = ["alex@simonov.me".freeze]
  s.homepage = "http://github.com/rubyzip/rubyzip".freeze
  s.licenses = ["BSD 2-Clause".freeze]
  s.required_ruby_version = Gem::Requirement.new(">= 1.9.2".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "rubyzip is a ruby module for reading and writing zip files".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_development_dependency(%q<rake>.freeze, ["~> 10.3"])
      s.add_development_dependency(%q<pry>.freeze, ["~> 0.10"])
      s.add_development_dependency(%q<minitest>.freeze, ["~> 5.4"])
      s.add_development_dependency(%q<coveralls>.freeze, ["~> 0.7"])
      s.add_development_dependency(%q<rubocop>.freeze, ["~> 0.49.1"])
    else
      s.add_dependency(%q<rake>.freeze, ["~> 10.3"])
      s.add_dependency(%q<pry>.freeze, ["~> 0.10"])
      s.add_dependency(%q<minitest>.freeze, ["~> 5.4"])
      s.add_dependency(%q<coveralls>.freeze, ["~> 0.7"])
      s.add_dependency(%q<rubocop>.freeze, ["~> 0.49.1"])
    end
  else
    s.add_dependency(%q<rake>.freeze, ["~> 10.3"])
    s.add_dependency(%q<pry>.freeze, ["~> 0.10"])
    s.add_dependency(%q<minitest>.freeze, ["~> 5.4"])
    s.add_dependency(%q<coveralls>.freeze, ["~> 0.7"])
    s.add_dependency(%q<rubocop>.freeze, ["~> 0.49.1"])
  end
end
