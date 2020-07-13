# frozen_string_literal: true

require File.expand_path("../lib/nio/version", __FILE__)

Gem::Specification.new do |spec|
  spec.authors       = ["Tony Arcieri"]
  spec.email         = ["bascule@gmail.com"]
  spec.homepage      = "https://github.com/socketry/nio4r"
  spec.license       = "MIT"
  spec.summary       = "New IO for Ruby"
  spec.description   = <<-DESCRIPTION.strip.gsub(/\s+/, " ")
     Cross-platform asynchronous I/O primitives for scalable network clients
     and servers. Inspired by the Java NIO API, but simplified for ease-of-use.
  DESCRIPTION

  spec.executables   = `git ls-files -- bin/*`.split("\n").map { |f| File.basename(f) }
  spec.files         = `git ls-files`.split("\n")
  spec.test_files    = `git ls-files -- {test,spec,features}/*`.split("\n")
  spec.name          = "nio4r"
  spec.require_paths = ["lib"]
  spec.version       = NIO::VERSION

  spec.metadata = {
    "bug_tracker_uri"   => "https://github.com/socketry/nio4r/issues",
    "changelog_uri"     => "https://github.com/socketry/nio4r/blob/master/CHANGES.md",
    "documentation_uri" => "https://www.rubydoc.info/gems/nio4r/#{spec.version}",
    "source_code_uri"   => "https://github.com/socketry/nio4r/tree/v#{spec.version}",
    "wiki_uri"          => "https://github.com/socketry/nio4r/wiki"
  }

  spec.required_ruby_version = ">= 2.3"

  if defined? JRUBY_VERSION
    spec.files << "lib/nio4r_ext.jar"
    spec.platform = "java"
  else
    spec.extensions = ["ext/nio4r/extconf.rb"]
  end

  spec.add_development_dependency "bundler"
  spec.add_development_dependency "rake"
end
