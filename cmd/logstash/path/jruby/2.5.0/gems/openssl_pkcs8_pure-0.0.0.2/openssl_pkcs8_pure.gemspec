# coding: utf-8

Gem::Specification.new do |spec|
  spec.name          = "openssl_pkcs8_pure"
  spec.version       = "0.0.0.2"
  spec.authors       = ["cielavenir"]
  spec.email         = ["cielartisan@gmail.com"]
  spec.description   = "OpenSSL::PKey::[DSA/RSA/EC]#to_pem_pkcs8 written in Ruby"
  spec.summary       = "OpenSSL::PKey::[DSA/RSA/EC]#to_pem_pkcs8 written in Ruby"
  spec.homepage      = "http://github.com/cielavenir/openssl_pkcs8_pure"
  spec.license       = "Ruby License (2-clause BSDL or Artistic)"

  spec.files         = `git ls-files`.split($/) + [
    "LICENSE.txt",
    "README.md",
    "CHANGELOG.md",
  ]
  spec.executables   = spec.files.grep(%r{^bin/}) { |f| File.basename(f) }
  spec.test_files    = spec.files.grep(%r{^(test|spec|features)/})

  spec.require_paths = ["lib"]

  spec.add_development_dependency "bundler", ">= 1.0"
  spec.add_development_dependency "rake"
  spec.add_development_dependency "rspec"
end
