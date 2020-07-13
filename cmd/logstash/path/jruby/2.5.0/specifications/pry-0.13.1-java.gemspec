# -*- encoding: utf-8 -*-
# stub: pry 0.13.1 java lib

Gem::Specification.new do |s|
  s.name = "pry".freeze
  s.version = "0.13.1"
  s.platform = "java".freeze

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "bug_tracker_uri" => "https://github.com/pry/pry/issues", "changelog_uri" => "https://github.com/pry/pry/blob/master/CHANGELOG.md", "source_code_uri" => "https://github.com/pry/pry" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["John Mair (banisterfiend)".freeze, "Conrad Irwin".freeze, "Ryan Fitzgerald".freeze, "Kyrylo Silin".freeze]
  s.date = "2020-04-12"
  s.description = "Pry is a runtime developer console and IRB alternative with powerful\nintrospection capabilities. Pry aims to be more than an IRB replacement. It is\nan attempt to bring REPL driven programming to the Ruby language.\n".freeze
  s.email = ["jrmair@gmail.com".freeze, "conrad.irwin@gmail.com".freeze, "rwfitzge@gmail.com".freeze, "silin@kyrylo.org".freeze]
  s.executables = ["pry".freeze]
  s.files = ["bin/pry".freeze]
  s.homepage = "http://pry.github.io".freeze
  s.licenses = ["MIT".freeze]
  s.required_ruby_version = Gem::Requirement.new(">= 1.9.3".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "A runtime developer console and IRB alternative with powerful introspection capabilities.".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<coderay>.freeze, ["~> 1.1"])
      s.add_runtime_dependency(%q<method_source>.freeze, ["~> 1.0"])
      s.add_runtime_dependency(%q<spoon>.freeze, ["~> 0.0"])
    else
      s.add_dependency(%q<coderay>.freeze, ["~> 1.1"])
      s.add_dependency(%q<method_source>.freeze, ["~> 1.0"])
      s.add_dependency(%q<spoon>.freeze, ["~> 0.0"])
    end
  else
    s.add_dependency(%q<coderay>.freeze, ["~> 1.1"])
    s.add_dependency(%q<method_source>.freeze, ["~> 1.0"])
    s.add_dependency(%q<spoon>.freeze, ["~> 0.0"])
  end
end
