# coding: utf-8

lib = File.expand_path('../lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)
require 'gems/version'

Gem::Specification.new do |spec|
  spec.version       = Gems::VERSION
  spec.name          = 'gems'
  spec.description   = 'Ruby wrapper for the RubyGems.org API'
  spec.summary       = spec.description

  spec.authors       = ['Erik Michaels-Ober']
  spec.email         = ['sferik@gmail.com']

  spec.files         = %w[.yardopts CONTRIBUTING.md LICENSE.md README.md gems.gemspec] + Dir['lib/**/*.rb']
  spec.homepage      = 'https://github.com/rubygems/gems'
  spec.licenses      = %w[MIT]
  spec.require_paths = %w[lib]
  spec.required_ruby_version = '>= 2.1.9'
end
