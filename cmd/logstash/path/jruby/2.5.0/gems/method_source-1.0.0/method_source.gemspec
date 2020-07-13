# -*- encoding: utf-8 -*-

Gem::Specification.new do |s|
  s.name = "method_source".freeze
  s.version = "1.0.0"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["John Mair (banisterfiend)".freeze]
  s.date = "2020-03-19"
  s.description = "retrieve the sourcecode for a method".freeze
  s.email = "jrmair@gmail.com".freeze
  s.files = ["CHANGELOG.md".freeze, ".gemtest".freeze, ".travis.yml".freeze, ".yardopts".freeze, "Gemfile".freeze, "LICENSE".freeze, "README.markdown".freeze, "Rakefile".freeze, "lib/method_source.rb".freeze, "lib/method_source/code_helpers.rb".freeze, "lib/method_source/source_location.rb".freeze, "lib/method_source/version.rb".freeze, "method_source.gemspec".freeze, "spec/method_source/code_helpers_spec.rb".freeze, "spec/method_source_spec.rb".freeze, "spec/spec_helper.rb".freeze]
  s.homepage = "http://banisterfiend.wordpress.com".freeze
  s.licenses = ["MIT".freeze]
  s.summary = "retrieve the sourcecode for a method".freeze
  s.test_files = ["spec/method_source/code_helpers_spec.rb".freeze, "spec/method_source_spec.rb".freeze, "spec/spec_helper.rb".freeze]

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_development_dependency(%q<rspec>.freeze, ["~> 3.6"])
      s.add_development_dependency(%q<rake>.freeze, ["~> 0.9"])
    else
      s.add_dependency(%q<rspec>.freeze, ["~> 3.6"])
      s.add_dependency(%q<rake>.freeze, ["~> 0.9"])
    end
  else
    s.add_dependency(%q<rspec>.freeze, ["~> 3.6"])
    s.add_dependency(%q<rake>.freeze, ["~> 0.9"])
  end
end
