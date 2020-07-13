# frozen_string_literal: true

require 'rubygems'
require 'rspec'
require 'hoe'

Hoe.plugin :bundler
Hoe.plugin :doofus
Hoe.plugin :gemspec2
Hoe.plugin :git

if RUBY_VERSION < '1.9'
  class Array #:nodoc:
    def to_h
      Hash[*flatten(1)]
    end
  end

  class Gem::Specification #:nodoc:
    def metadata=(*); end

    def default_value(*); end
  end

  class Object #:nodoc:
    def caller_locations(*)
      []
    end
  end
end

_spec = Hoe.spec 'diff-lcs' do
  developer('Austin Ziegler', 'halostatue@gmail.com')

  require_ruby_version '>= 1.8'

  self.history_file = 'History.md'
  self.readme_file = 'README.rdoc'
  self.licenses = ['MIT', 'Artistic-2.0', 'GPL-2.0+']

  extra_dev_deps << ['hoe-doofus', '~> 1.0']
  extra_dev_deps << ['hoe-gemspec2', '~> 1.1']
  extra_dev_deps << ['hoe-git', '~> 1.6']
  extra_dev_deps << ['hoe-rubygems', '~> 1.0']
  extra_dev_deps << ['rspec', '>= 2.0', '< 4']
  extra_dev_deps << ['rake', '>= 10.0', '< 14']
  extra_dev_deps << ['rdoc', '>= 0']
end

if RUBY_VERSION >= '2.0' && RUBY_ENGINE == 'ruby'
  namespace :spec do
    desc "Runs test coverage. Only works Ruby 2.0+ and assumes 'simplecov' is installed."
    task :coverage do
      ENV['COVERAGE'] = 'yes'
      Rake::Task['spec'].execute
    end
  end
end

task :ruby18 do
  puts <<-MESSAGE
You are starting a barebones Ruby 1.8 docker environment. You will need to
do the following:

- mv Gemfile.lock{,.v2}
- gem install bundler --version 1.17.2 --no-ri --no-rdoc
- ruby -S bundle
- rake

Don't forget to restore your Gemfile.lock after testing.

  MESSAGE
  sh "docker run -it --rm -v #{Dir.pwd}:/root/diff-lcs bellbind/docker-ruby18-rails2 bash -l"
end
