# -*- encoding: utf-8 -*-
# stub: manticore 0.6.4 java lib

Gem::Specification.new do |s|
  s.name = "manticore".freeze
  s.version = "0.6.4"
  s.platform = "java".freeze

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["Chris Heald".freeze]
  s.date = "2018-06-28"
  s.description = "Manticore is an HTTP client built on the Apache HttpCore components".freeze
  s.email = ["cheald@mashable.com".freeze]
  s.homepage = "https://github.com/cheald/manticore".freeze
  s.licenses = ["MIT".freeze]
  s.requirements = ["jar org.apache.httpcomponents:httpclient, '~> 4.5.0'".freeze, "jar org.apache.httpcomponents:httpmime,   '~> 4.5.0'".freeze, "jar commons-logging:commons-logging,      '~> 1.2'".freeze, "jar commons-codec:commons-codec,          '~> 1.9'".freeze, "jar org.apache.httpcomponents:httpcore,   '~> 4.4.4'".freeze]
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Manticore is an HTTP client built on the Apache HttpCore components".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<openssl_pkcs8_pure>.freeze, [">= 0"])
      s.add_development_dependency(%q<bundler>.freeze, ["~> 1.3"])
      s.add_development_dependency(%q<rake>.freeze, [">= 0"])
      s.add_development_dependency(%q<jar-dependencies>.freeze, [">= 0"])
    else
      s.add_dependency(%q<openssl_pkcs8_pure>.freeze, [">= 0"])
      s.add_dependency(%q<bundler>.freeze, ["~> 1.3"])
      s.add_dependency(%q<rake>.freeze, [">= 0"])
      s.add_dependency(%q<jar-dependencies>.freeze, [">= 0"])
    end
  else
    s.add_dependency(%q<openssl_pkcs8_pure>.freeze, [">= 0"])
    s.add_dependency(%q<bundler>.freeze, ["~> 1.3"])
    s.add_dependency(%q<rake>.freeze, [">= 0"])
    s.add_dependency(%q<jar-dependencies>.freeze, [">= 0"])
  end
end
