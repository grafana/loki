# coding: utf-8
lib = File.expand_path('../lib', __FILE__)
$LOAD_PATH.unshift(lib) unless $LOAD_PATH.include?(lib)
require 'manticore/version'

Gem::Specification.new do |spec|
  spec.name          = "manticore"
  spec.version       = Manticore::VERSION
  spec.authors       = ["Chris Heald"]
  spec.email         = ["cheald@mashable.com"]
  spec.description   = %q{Manticore is an HTTP client built on the Apache HttpCore components}
  spec.summary       = %q{Manticore is an HTTP client built on the Apache HttpCore components}
  spec.homepage      = "https://github.com/cheald/manticore"
  spec.license       = "MIT"
  spec.platform      = 'java'

  spec.files         = `git ls-files`.split($/)
  spec.executables   = spec.files.grep(%r{^bin/}) { |f| File.basename(f) }
  spec.test_files    = spec.files.grep(%r{^(test|spec|features)/})
  spec.require_paths = ["lib"]

  private_key = File.expand_path("~/.gemcert/gem-private_key.pem")
  if File.exists? private_key
    spec.signing_key = private_key
    spec.cert_chain  = ['gem-public_cert.pem']
  end

  spec.add_dependency "openssl_pkcs8_pure"

  spec.add_development_dependency "bundler", "~> 1.3"
  spec.add_development_dependency "rake"
  spec.add_development_dependency "jar-dependencies"

  spec.requirements << "jar org.apache.httpcomponents:httpclient, '~> 4.5.0'"
  spec.requirements << "jar org.apache.httpcomponents:httpmime,   '~> 4.5.0'"
  spec.requirements << "jar commons-logging:commons-logging,      '~> 1.2'"
  spec.requirements << "jar commons-codec:commons-codec,          '~> 1.9'"
  spec.requirements << "jar org.apache.httpcomponents:httpcore,   '~> 4.4.4'"
end
