# -*- encoding: utf-8 -*-
# stub: polyglot 0.3.5 ruby lib

Gem::Specification.new do |s|
  s.name = "polyglot".freeze
  s.version = "0.3.5"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["Clifford Heath".freeze]
  s.date = "2014-05-30"
  s.description = "\nThe Polyglot library allows a Ruby module to register a loader\nfor the file type associated with a filename extension, and it\naugments 'require' to find and load matching files.".freeze
  s.email = ["clifford.heath@gmail.com".freeze]
  s.extra_rdoc_files = ["README.txt".freeze]
  s.files = ["README.txt".freeze]
  s.homepage = "http://github.com/cjheath/polyglot".freeze
  s.licenses = ["MIT".freeze]
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Augment 'require' to load non-Ruby file types".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version
end
