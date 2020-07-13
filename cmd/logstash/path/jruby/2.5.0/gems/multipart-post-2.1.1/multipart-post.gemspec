# -*- encoding: utf-8 -*-
$:.push File.expand_path("../lib", __FILE__)
require "multipart_post"

Gem::Specification.new do |spec|
  spec.name        = "multipart-post"
  spec.version     = MultipartPost::VERSION
  spec.authors     = ["Nick Sieger", "Samuel Williams"]
  spec.email       = ["nick@nicksieger.com", "samuel.williams@oriontransfer.co.nz"]
  spec.homepage    = "https://github.com/nicksieger/multipart-post"
  spec.summary     = %q{A multipart form post accessory for Net::HTTP.}
  spec.license     = "MIT"
  spec.description = %q{Use with Net::HTTP to do multipart form postspec. IO values that have #content_type, #original_filename, and #local_path will be posted as a binary file.}
  
  spec.files         = `git ls-files`.split("\n")
  spec.test_files    = `git ls-files -- {test,spec,features}/*`.split("\n")
  spec.executables   = `git ls-files -- bin/*`.split("\n").map{ |f| File.basename(f) }
  spec.require_paths = ["lib"]
  
  spec.add_development_dependency 'bundler', ['>= 1.3', '< 3']
  spec.add_development_dependency 'rspec', '~> 3.4'
  spec.add_development_dependency 'rake'
end
