require_relative 'lib/concurrent-ruby/concurrent/version'
require_relative 'lib/concurrent-ruby-edge/concurrent/edge/version'
require_relative 'lib/concurrent-ruby/concurrent/utility/engine'

if Concurrent.ruby_version :<, 2, 0, 0
  # @!visibility private
  module Kernel
    def __dir__
      File.dirname __FILE__
    end
  end
end

core_gemspec = Gem::Specification.load File.join(__dir__, 'concurrent-ruby.gemspec')
ext_gemspec  = Gem::Specification.load File.join(__dir__, 'concurrent-ruby-ext.gemspec')
edge_gemspec = Gem::Specification.load File.join(__dir__, 'concurrent-ruby-edge.gemspec')

require 'rake/javaextensiontask'

ENV['JRUBY_HOME'] = ENV['CONCURRENT_JRUBY_HOME'] if ENV['CONCURRENT_JRUBY_HOME'] && !Concurrent.on_jruby?

Rake::JavaExtensionTask.new('concurrent_ruby', core_gemspec) do |ext|
  ext.ext_dir = 'ext/concurrent-ruby'
  ext.lib_dir = 'lib/concurrent-ruby/concurrent'
end

unless Concurrent.on_jruby?
  require 'rake/extensiontask'

  Rake::ExtensionTask.new('concurrent_ruby_ext', ext_gemspec) do |ext|
    ext.ext_dir        = 'ext/concurrent-ruby-ext'
    ext.lib_dir        = 'lib/concurrent-ruby/concurrent'
    ext.source_pattern = '*.{c,h}'

    ext.cross_compile  = true
    ext.cross_platform = ['x86-mingw32', 'x64-mingw32']
  end
end

require 'rake_compiler_dock'
namespace :repackage do
  desc '* with Windows fat distributions'
  task :all do
    Dir.chdir(__dir__) do
      # store gems in vendor cache for docker
      sh 'bundle package'

      # build only the jar file not the whole gem for java platform, the jar is part the concurrent-ruby-x.y.z.gem
      Rake::Task['lib/concurrent-ruby/concurrent/concurrent_ruby.jar'].invoke

      # build all gem files
      RakeCompilerDock.sh 'bundle install --local && bundle exec rake cross native package --trace'
    end
  end
end

require 'rubygems'
require 'rubygems/package_task'

Gem::PackageTask.new(core_gemspec) {} if core_gemspec
Gem::PackageTask.new(ext_gemspec) {} if ext_gemspec && !Concurrent.on_jruby?
Gem::PackageTask.new(edge_gemspec) {} if edge_gemspec

CLEAN.include('lib/concurrent-ruby/concurrent/2.*', 'lib/concurrent-ruby/concurrent/*.jar')

begin
  require 'rspec'
  require 'rspec/core/rake_task'

  RSpec::Core::RakeTask.new(:spec)

  namespace :spec do
    desc '* Configured for ci'
    RSpec::Core::RakeTask.new(:ci) do |t|
      options      = %w[ --color
                    --backtrace
                    --order defined
                    --format documentation
                    --tag ~notravis ]
      t.rspec_opts = [*options].join(' ')
    end

    desc '* test packaged and installed gems instead of local files'
    task :installed do
      Dir.chdir(__dir__) do
        sh "gem install pkg/concurrent-ruby-#{Concurrent::VERSION}.gem"
        sh "gem install pkg/concurrent-ruby-ext-#{Concurrent::VERSION}.gem" if Concurrent.on_cruby?
        sh "gem install pkg/concurrent-ruby-edge-#{Concurrent::EDGE_VERSION}.gem"
        ENV['NO_PATH'] = 'true'
        sh 'bundle update'
        sh 'bundle exec rake spec:ci'
      end
    end
  end

  desc 'executed in CI'
  task :ci => [:compile, 'spec:ci']

  task :default => [:clobber, :compile, :spec]
rescue LoadError => e
  puts 'RSpec is not installed, skipping test task definitions: ' + e.message
end

current_yard_version_name = Concurrent::VERSION

begin
  require 'yard'
  require 'md_ruby_eval'
  require_relative 'support/yard_full_types'

  common_yard_options = ['--no-yardopts',
                         '--no-document',
                         '--no-private',
                         '--embed-mixins',
                         '--markup', 'markdown',
                         '--title', 'Concurrent Ruby',
                         '--template', 'default',
                         '--template-path', 'yard-template',
                         '--default-return', 'undocumented']

  desc 'Generate YARD Documentation (signpost, master)'
  task :yard => ['yard:signpost', 'yard:master']

  namespace :yard do

    desc '* eval markdown files'
    task :eval_md do
      Dir.chdir File.join(__dir__, 'docs-source') do
        sh 'bundle exec md-ruby-eval --auto'
      end
    end

    task :update_readme do
      Dir.chdir __dir__ do
        content = File.read(File.join('README.md')).
            gsub(/\[([\w ]+)\]\(http:\/\/ruby-concurrency\.github\.io\/concurrent-ruby\/master\/.*\)/) do |_|
          case $1
          when 'LockFreeLinkedSet'
            "{Concurrent::Edge::#{$1} #{$1}}"
          when '.dataflow'
            '{Concurrent.dataflow Concurrent.dataflow}'
          when 'thread pool'
            '{file:thread_pools.md thread pool}'
          else
            "{Concurrent::#{$1} #{$1}}"
          end
        end
        FileUtils.mkpath 'tmp'
        File.write 'tmp/README.md', content
      end
    end

    define_yard_task = -> name do
      output_dir = "docs/#{name}"

      removal_name = "remove.#{name}"
      task removal_name do
        Dir.chdir __dir__ do
          FileUtils.rm_rf output_dir
        end
      end

      desc "* of #{name} into subdir #{name}"
      YARD::Rake::YardocTask.new(name) do |yard|
        yard.options.push(
            '--output-dir', output_dir,
            '--main', 'tmp/README.md',
            *common_yard_options)
        yard.files = ['./lib/concurrent-ruby/**/*.rb',
                      './lib/concurrent-ruby-edge/**/*.rb',
                      './ext/concurrent_ruby_ext/**/*.c',
                      '-',
                      'docs-source/thread_pools.md',
                      'docs-source/promises.out.md',
                      'docs-source/medium-example.out.rb',
                      'LICENSE.md',
                      'CHANGELOG.md']
      end
      Rake::Task[name].prerequisites.push removal_name,
                                          # 'yard:eval_md',
                                          'yard:update_readme'
    end

    define_yard_task.call current_yard_version_name
    define_yard_task.call 'master'

    desc "* signpost for versions"
    YARD::Rake::YardocTask.new(:signpost) do |yard|
      yard.options.push(
          '--output-dir', 'docs',
          '--main', 'docs-source/signpost.md',
          *common_yard_options)
      yard.files = ['no-lib']
    end

    define_uptodate_task = -> name do
      namespace name do
        desc "** ensure that #{name} generated documentation is matching the source code"
        task :uptodate do
          Dir.chdir(__dir__) do
            begin
              FileUtils.cp_r 'docs', 'docs-copy', verbose: true
              Rake::Task["yard:#{name}"].invoke
              sh 'diff -r docs/ docs-copy/' do |ok, res|
                unless ok
                  begin
                    STDOUT.puts 'Command failed. Continue? (y/n)'
                    input = STDIN.gets.strip.downcase
                  end until %w(y n).include?(input)
                  exit 1 if input == 'n'
                end
              end
            ensure
              FileUtils.rm_rf 'docs-copy', verbose: true
            end
          end
        end
      end
    end

    define_uptodate_task.call current_yard_version_name
    define_uptodate_task.call 'master'
  end

rescue LoadError => e
  puts 'YARD is not installed, skipping documentation task definitions: ' + e.message
end

desc 'build, test, and publish the gem'
task :release => ['release:checks', 'release:build', 'release:test', 'release:publish']

namespace :release do
  # Depends on environment of @pitr-ch

  mri_version   = '2.6.5'
  jruby_version = 'jruby-9.2.9.0'

  task :checks => "yard:#{current_yard_version_name}:uptodate" do
    Dir.chdir(__dir__) do
      sh 'test -z "$(git status --porcelain)"' do |ok, res|
        unless ok
          begin
            STDOUT.puts 'Command failed. Continue? (y/n)'
            input = STDIN.gets.strip.downcase
          end until %w(y n).include?(input)
          exit 1 if input == 'n'
        end
      end
      sh 'git fetch'
      sh 'test $(git show-ref --verify --hash refs/heads/master) = ' +
             '$(git show-ref --verify --hash refs/remotes/origin/master)' do |ok, res|
        unless ok
          begin
            STDOUT.puts 'Command failed. Continue? (y/n)'
            input = STDIN.gets.strip.downcase
          end until %w(y n).include?(input)
          exit 1 if input == 'n'
        end
      end
    end
  end

  desc '* build all *.gem files necessary for release'
  task :build => [:clobber, 'repackage:all']

  desc '* test actual installed gems instead of cloned repository on MRI and JRuby'
  task :test do
    Dir.chdir(__dir__) do
      old = ENV['RBENV_VERSION']

      ENV['RBENV_VERSION'] = mri_version
      sh 'rbenv version'
      sh 'bundle exec rake spec:installed'

      ENV['RBENV_VERSION'] = jruby_version
      sh 'rbenv version'
      sh 'bundle exec rake spec:installed'

      puts 'Windows build is untested'

      ENV['RBENV_VERSION'] = old
    end
  end

  desc '* do all nested steps'
  task :publish => ['publish:ask', 'publish:tag', 'publish:rubygems', 'publish:post_steps']

  namespace :publish do
    publish_edge = false

    task :ask do
      begin
        STDOUT.puts 'Do you want to publish anything? (y/n)'
        input = STDIN.gets.strip.downcase
      end until %w(y n).include?(input)
      exit 1 if input == 'n'
      begin
        STDOUT.puts 'Do you want to publish edge? (y/n)'
        input = STDIN.gets.strip.downcase
      end until %w(y n).include?(input)
      publish_edge = input == 'y'
    end

    desc '** tag HEAD with current version and push to github'
    task :tag do
      Dir.chdir(__dir__) do
        sh "git tag v#{Concurrent::VERSION}"
        sh "git push origin v#{Concurrent::VERSION}"
        sh "git tag edge-v#{Concurrent::EDGE_VERSION}" if publish_edge
        sh "git push origin edge-v#{Concurrent::EDGE_VERSION}" if publish_edge
      end
    end

    desc '** push all *.gem files to rubygems'
    task :rubygems do
      Dir.chdir(__dir__) do
        sh "gem push pkg/concurrent-ruby-#{Concurrent::VERSION}.gem"
        sh "gem push pkg/concurrent-ruby-edge-#{Concurrent::EDGE_VERSION}.gem" if publish_edge
        sh "gem push pkg/concurrent-ruby-ext-#{Concurrent::VERSION}.gem"
        sh "gem push pkg/concurrent-ruby-ext-#{Concurrent::VERSION}-x64-mingw32.gem"
        sh "gem push pkg/concurrent-ruby-ext-#{Concurrent::VERSION}-x86-mingw32.gem"
      end
    end

    desc '** print post release steps'
    task :post_steps do
      puts 'Manually: create a release on GitHub with relevant changelog part'
      puts 'Manually: send email same as release with relevant changelog part'
      puts 'Manually: tweet'
    end
  end
end
