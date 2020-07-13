# -*- encoding: utf-8 -*-
lib = File.expand_path('../lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)
require 'date'
require 'treetop/version'

Gem::Specification.new do |spec|
  spec.name = "treetop"
  spec.version = Treetop::VERSION::STRING
  spec.authors = ["Nathan Sobo", "Clifford Heath"]

  spec.email = "cliffordheath@gmail.com"
  spec.date = Date.today.strftime("%F")
  spec.summary = "A Ruby-based text parsing and interpretation DSL"
  spec.description = "A Parsing Expression Grammar (PEG) Parser generator DSL for Ruby"
  spec.homepage = "https://github.com/cjheath/treetop"
  spec.licenses = ["MIT"]

  spec.files = `git ls-files -z`.split("\x0").reject { |f| f.match(%r{^(spec|website|script|\.|benchmark)}) }
  spec.executables = ["tt"]
  spec.require_paths = ["lib"]
  spec.extra_rdoc_files = [
    "LICENSE",
    "README.md"
  ]

  spec.add_runtime_dependency(%q<polyglot>, ["~> 0.3"])
  spec.add_development_dependency(%q<activesupport>, ["~> 4"])
  spec.add_development_dependency(%q<i18n>, ["~> 0.6"])
  spec.add_development_dependency(%q<rr>, ["~> 1.0"])
  spec.add_development_dependency(%q<rspec>, ["~> 3"])
  spec.add_development_dependency(%q<rake>, ["~> 11"])
end

