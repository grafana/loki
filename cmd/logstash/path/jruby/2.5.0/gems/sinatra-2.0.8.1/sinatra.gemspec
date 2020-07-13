version = File.read(File.expand_path("../VERSION", __FILE__)).strip

Gem::Specification.new 'sinatra', version do |s|
  s.description       = "Sinatra is a DSL for quickly creating web applications in Ruby with minimal effort."
  s.summary           = "Classy web-development dressed in a DSL"
  s.authors           = ["Blake Mizerany", "Ryan Tomayko", "Simon Rozet", "Konstantin Haase"]
  s.email             = "sinatrarb@googlegroups.com"
  s.homepage          = "http://sinatrarb.com/"
  s.license           = 'MIT'
  s.files             = Dir['README*.md', 'lib/**/*', 'examples/*'] + [
    ".yardopts",
    "AUTHORS.md",
    "CHANGELOG.md",
    "CONTRIBUTING.md",
    "Gemfile",
    "LICENSE",
    "MAINTENANCE.md",
    "Rakefile",
    "SECURITY.md",
    "sinatra.gemspec",
    "VERSION"]
  s.test_files        = s.files.select { |p| p =~ /^test\/.*_test.rb/ }
  s.extra_rdoc_files  = s.files.select { |p| p =~ /^README/ } << 'LICENSE'
  s.rdoc_options      = %w[--line-numbers --inline-source --title Sinatra --main README.rdoc --encoding=UTF-8]

  if s.respond_to?(:metadata)
    s.metadata = {
      'source_code_uri'   => 'https://github.com/sinatra/sinatra',
      'changelog_uri'     => 'https://github.com/sinatra/sinatra/blob/master/CHANGELOG.md',
      'homepage_uri'      => 'http://sinatrarb.com/',
      'bug_tracker_uri'   => 'https://github.com/sinatra/sinatra/issues',
      'mailing_list_uri'  => 'http://groups.google.com/group/sinatrarb',
      'documentation_uri' => 'https://www.rubydoc.info/gems/sinatra'
    }
  else
    msg = "RubyGems 2.0 or newer is required to protect against public "\
          "gem pushes. You can update your rubygems version by running:\n\n"\
          "gem install rubygems-update\n"\
          "update_rubygems\n"\
          "gem update --system"
    raise <<-EOF
RubyGems 2.0 or newer is required to protect against public gem pushes. You can update your rubygems version by running:
  gem install rubygems-update
  update_rubygems:
  gem update --system
EOF
  end

  s.required_ruby_version = '>= 2.2.0'

  s.add_dependency 'rack', '~> 2.0'
  s.add_dependency 'tilt', '~> 2.0'
  s.add_dependency 'rack-protection', version
  s.add_dependency 'mustermann', '~> 1.0'
end
