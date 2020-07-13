# -*- encoding: utf-8 -*-
# stub: openssl_pkcs8_pure 0.0.0.2 ruby lib

Gem::Specification.new do |s|
  s.name = "openssl_pkcs8_pure".freeze
  s.version = "0.0.0.2"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["cielavenir".freeze]
  s.date = "2017-03-02"
  s.description = "OpenSSL::PKey::[DSA/RSA/EC]#to_pem_pkcs8 written in Ruby".freeze
  s.email = ["cielartisan@gmail.com".freeze]
  s.homepage = "http://github.com/cielavenir/openssl_pkcs8_pure".freeze
  s.licenses = ["Ruby License (2-clause BSDL or Artistic)".freeze]
  s.rubygems_version = "3.0.6".freeze
  s.summary = "OpenSSL::PKey::[DSA/RSA/EC]#to_pem_pkcs8 written in Ruby".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_development_dependency(%q<bundler>.freeze, [">= 1.0"])
      s.add_development_dependency(%q<rake>.freeze, [">= 0"])
      s.add_development_dependency(%q<rspec>.freeze, [">= 0"])
    else
      s.add_dependency(%q<bundler>.freeze, [">= 1.0"])
      s.add_dependency(%q<rake>.freeze, [">= 0"])
      s.add_dependency(%q<rspec>.freeze, [">= 0"])
    end
  else
    s.add_dependency(%q<bundler>.freeze, [">= 1.0"])
    s.add_dependency(%q<rake>.freeze, [">= 0"])
    s.add_dependency(%q<rspec>.freeze, [">= 0"])
  end
end
