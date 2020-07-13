version = File.read(File.expand_path("../../VERSION", __FILE__)).strip

Gem::Specification.new do |s|
  # general infos
  s.name        = "rack-protection"
  s.version     = version
  s.description = "Protect against typical web attacks, works with all Rack apps, including Rails."
  s.homepage    = "http://sinatrarb.com/protection/"
  s.summary     = s.description
  s.license     = 'MIT'
  s.authors     = ["https://github.com/sinatra/sinatra/graphs/contributors"]
  s.email       = "sinatrarb@googlegroups.com"
  s.files       = Dir["lib/**/*.rb"] + [
    "License",
    "README.md",
    "Rakefile",
    "Gemfile",
    "rack-protection.gemspec"
  ]

  if s.respond_to?(:metadata)
    s.metadata = {
      'source_code_uri'   => 'https://github.com/sinatra/sinatra/tree/master/rack-protection',
      'homepage_uri'      => 'http://sinatrarb.com/protection/',
      'documentation_uri' => 'https://www.rubydoc.info/gems/rack-protection'
    }
  else
    raise <<-EOF
RubyGems 2.0 or newer is required to protect against public gem pushes. You can update your rubygems version by running:
  gem install rubygems-update
  update_rubygems:
  gem update --system
EOF
  end

  # dependencies
  s.add_dependency "rack"
  s.add_development_dependency "rack-test"
  s.add_development_dependency "rspec", "~> 3.6"
end
