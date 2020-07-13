# -*- ruby encoding: utf-8 -*-

require 'rubygems'
require 'hoe'
require 'rake/clean'

$LOAD_PATH.unshift('support')

Hoe.plugin :doofus
Hoe.plugin :gemspec2
Hoe.plugin :git
Hoe.plugin :minitest
Hoe.plugin :travis
Hoe.plugin :deprecated_gem
Hoe.plugin :email unless ENV['CI'] or ENV['TRAVIS']

spec = Hoe.spec 'minitar' do
  developer('Austin Ziegler', 'halostatue@gmail.com')

  require_ruby_version '>= 1.8'

  self.history_file = 'History.md'
  self.readme_file = 'README.rdoc'
  self.licenses = ['Ruby', 'BSD-2-Clause']

  self.post_install_message = <<-EOS
The `minitar` executable is no longer bundled with `minitar`. If you are
expecting this executable, make sure you also install `minitar-cli`.
  EOS

  extra_dev_deps << ['hoe-doofus', '~> 1.0']
  extra_dev_deps << ['hoe-gemspec2', '~> 1.1']
  extra_dev_deps << ['hoe-git', '~> 1.6']
  extra_dev_deps << ['hoe-rubygems', '~> 1.0']
  extra_dev_deps << ['hoe-travis', '~> 1.2']
  extra_dev_deps << ['minitest', '~> 5.3']
  extra_dev_deps << ['minitest-autotest', ['>= 1.0', '<2']]
  extra_dev_deps << ['rake', '>= 10.0', '< 12']
  extra_dev_deps << ['rdoc', '>= 0.0']
end

if RUBY_VERSION >= '2.0' && RUBY_ENGINE == 'ruby'
  namespace :test do
    desc 'Run test coverage'
    task :coverage do
      spec.test_prelude = 'load ".simplecov-prelude.rb"'
      Rake::Task['test'].execute
    end
  end

  Rake::Task['travis'].prerequisites.replace(%w(test:coverage))
end
