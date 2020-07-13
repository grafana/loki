# -*- encoding: utf-8 -*-
$:.push File.expand_path("../lib", __FILE__)
require "clamp/version"

Gem::Specification.new do |s|

  s.name          = "clamp"
  s.version       = Clamp::VERSION.dup
  s.platform      = Gem::Platform::RUBY
  s.authors       = ["Mike Williams"]
  s.email         = "mdub@dogbiscuit.org"
  s.homepage      = "http://github.com/mdub/clamp"

  s.license       = 'MIT'

  s.summary       = %q{a minimal framework for command-line utilities}
  s.description   = <<EOF
Clamp provides an object-model for command-line utilities.
It handles parsing of command-line options, and generation of usage help.
EOF

  s.files         = `git ls-files`.split("\n")
  s.test_files    = `git ls-files -- {test,spec,features}/*`.split("\n")
  s.require_paths = ["lib"]

end
