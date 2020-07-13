# -*- encoding: utf-8 -*-
# stub: sinatra 2.0.8.1 ruby lib

Gem::Specification.new do |s|
  s.name = "sinatra".freeze
  s.version = "2.0.8.1"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "bug_tracker_uri" => "https://github.com/sinatra/sinatra/issues", "changelog_uri" => "https://github.com/sinatra/sinatra/blob/master/CHANGELOG.md", "documentation_uri" => "https://www.rubydoc.info/gems/sinatra", "homepage_uri" => "http://sinatrarb.com/", "mailing_list_uri" => "http://groups.google.com/group/sinatrarb", "source_code_uri" => "https://github.com/sinatra/sinatra" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["Blake Mizerany".freeze, "Ryan Tomayko".freeze, "Simon Rozet".freeze, "Konstantin Haase".freeze]
  s.date = "2020-01-01"
  s.description = "Sinatra is a DSL for quickly creating web applications in Ruby with minimal effort.".freeze
  s.email = "sinatrarb@googlegroups.com".freeze
  s.extra_rdoc_files = ["README.de.md".freeze, "README.es.md".freeze, "README.fr.md".freeze, "README.hu.md".freeze, "README.ja.md".freeze, "README.ko.md".freeze, "README.malayalam.md".freeze, "README.md".freeze, "README.pt-br.md".freeze, "README.pt-pt.md".freeze, "README.ru.md".freeze, "README.zh.md".freeze, "LICENSE".freeze]
  s.files = ["LICENSE".freeze, "README.de.md".freeze, "README.es.md".freeze, "README.fr.md".freeze, "README.hu.md".freeze, "README.ja.md".freeze, "README.ko.md".freeze, "README.malayalam.md".freeze, "README.md".freeze, "README.pt-br.md".freeze, "README.pt-pt.md".freeze, "README.ru.md".freeze, "README.zh.md".freeze]
  s.homepage = "http://sinatrarb.com/".freeze
  s.licenses = ["MIT".freeze]
  s.rdoc_options = ["--line-numbers".freeze, "--inline-source".freeze, "--title".freeze, "Sinatra".freeze, "--main".freeze, "README.rdoc".freeze, "--encoding=UTF-8".freeze]
  s.required_ruby_version = Gem::Requirement.new(">= 2.2.0".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Classy web-development dressed in a DSL".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<rack>.freeze, ["~> 2.0"])
      s.add_runtime_dependency(%q<tilt>.freeze, ["~> 2.0"])
      s.add_runtime_dependency(%q<rack-protection>.freeze, ["= 2.0.8.1"])
      s.add_runtime_dependency(%q<mustermann>.freeze, ["~> 1.0"])
    else
      s.add_dependency(%q<rack>.freeze, ["~> 2.0"])
      s.add_dependency(%q<tilt>.freeze, ["~> 2.0"])
      s.add_dependency(%q<rack-protection>.freeze, ["= 2.0.8.1"])
      s.add_dependency(%q<mustermann>.freeze, ["~> 1.0"])
    end
  else
    s.add_dependency(%q<rack>.freeze, ["~> 2.0"])
    s.add_dependency(%q<tilt>.freeze, ["~> 2.0"])
    s.add_dependency(%q<rack-protection>.freeze, ["= 2.0.8.1"])
    s.add_dependency(%q<mustermann>.freeze, ["~> 1.0"])
  end
end
