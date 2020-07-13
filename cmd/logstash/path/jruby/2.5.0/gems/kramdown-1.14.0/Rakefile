# -*- ruby -*-

# load all optional developer libraries
begin
  require 'rubygems'
  require 'rubygems/package_task'
rescue LoadError
end

begin
  require 'webgen/page'
rescue LoadError
end

begin
  gem 'rdoc' if RUBY_VERSION >= '1.9'
  require 'rdoc/task'
  require 'rdoc/rdoc'

  class RDoc::RDoc

    alias :old_parse_files :parse_files

    def parse_files(options)
      file_info = old_parse_files(options)
      require 'kramdown/options'

      # Add options documentation to Kramdown::Options module
      opt_module = @store.all_classes_and_modules.find {|m| m.full_name == 'Kramdown::Options'}
      opt_defs = Kramdown::Options.definitions.sort.collect do |n, definition|
        desc = definition.desc.split(/\n/).map {|l| "    #{l}"}
        desc[-2] = []
        desc = desc.join("\n")
        "[<tt>#{n}</tt> (type: #{definition.type}, default: #{definition.default.inspect})]\n#{desc}\n\n"
      end
      opt_module.comment.text += "\n== Available Options\n\n" << opt_defs.join("\n\n")

      file_info
    end

  end

rescue LoadError
end

begin
  require 'rcov/rcovtask'
rescue LoadError
end

require 'fileutils'
require 'rake/clean'
require 'rake/testtask'
require 'rake/packagetask'
require 'erb'

$:.unshift('lib')
require 'kramdown'

# End user tasks ################################################################

task :default => :test

desc "Install using setup.rb"
task :install do
  ruby "setup.rb config"
  ruby "setup.rb setup"
  ruby "setup.rb install"
end

task :clobber do
  ruby "setup.rb clean"
end

if defined?(Webgen)
  desc "Generate the HTML documentation"
  task :htmldoc do
    ruby "-Ilib -S webgen"
  end
  CLOBBER << "htmldoc/"
  CLOBBER << "webgen-tmp"
end

if defined? RDoc::Task
  rd = RDoc::Task.new do |rdoc|
    rdoc.rdoc_dir = 'htmldoc/rdoc'
    rdoc.title = 'kramdown'
    rdoc.main = 'lib/kramdown/document.rb'
    rdoc.rdoc_files.include('lib')
  end
end

if defined?(Webgen) && defined?(RDoc::Task)
  desc "Build the whole user documentation"
  task :doc => [:rdoc, 'htmldoc']
end

tt = Rake::TestTask.new do |test|
  test.warning = false
  test.libs << 'test'
  test.test_files = FileList['test/test_*.rb']
end

# Release tasks and development tasks ############################################

namespace :dev do

  SUMMARY = 'kramdown is a fast, pure-Ruby Markdown-superset converter.'
  DESCRIPTION = <<EOF
kramdown is yet-another-markdown-parser but fast, pure Ruby,
using a strict syntax definition and supporting several common extensions.
EOF

  begin
    REL_PAGE = Webgen::Page.from_data(File.read('doc/news/release_' + Kramdown::VERSION.split('.').join('_') + '.page'))
  rescue
    puts 'NO RELEASE NOTES/CHANGES FILE'
  end

  PKG_FILES = FileList.new([
                            'Rakefile',
                            'setup.rb',
                            'COPYING', 'README.md', 'AUTHORS',
                            'VERSION', 'CONTRIBUTERS',
                            'bin/*',
                            'benchmark/*',
                            'lib/**/*.rb',
                            'man/man1/kramdown.1',
                            'data/**/*',
                            'doc/**',
                            'test/**/*'
                           ])

  CLOBBER << "VERSION"
  file 'VERSION' do
    puts "Generating VERSION file"
    File.open('VERSION', 'w+') {|file| file.write(Kramdown::VERSION + "\n")}
  end

  CLOBBER << 'CONTRIBUTERS'
  file 'CONTRIBUTERS' do
    puts "Generating CONTRIBUTERS file"
    `echo "  Count Name" > CONTRIBUTERS`
    `echo "======= ====" >> CONTRIBUTERS`
    `git log | grep ^Author: | sed 's/^Author: //' | sort | uniq -c | sort -nr >> CONTRIBUTERS`
  end

  CLOBBER << "man/man1/kramdown.1"
  file 'man/man1/kramdown.1' => ['man/man1/kramdown.1.erb'] do
    puts "Generating kramdown man page"
    File.open('man/man1/kramdown.1', 'w+') do |file|
      data = ERB.new(File.read('man/man1/kramdown.1.erb')).result(binding)
      file.write(Kramdown::Document.new(data).to_man)
    end
  end

  Rake::PackageTask.new('kramdown', Kramdown::VERSION) do |pkg|
    pkg.need_tar = true
    pkg.need_zip = true
    pkg.package_files = PKG_FILES
  end

  if defined? Gem
    spec = Gem::Specification.new do |s|

      #### Basic information
      s.name = 'kramdown'
      s.version = Kramdown::VERSION
      s.summary = SUMMARY
      s.description = DESCRIPTION
      s.license = 'MIT'

      #### Dependencies, requirements and files
      s.files = PKG_FILES.to_a

      s.require_path = 'lib'
      s.executables = ['kramdown']
      s.default_executable = 'kramdown'
      s.add_development_dependency 'minitest', '~> 5.0'
      s.add_development_dependency 'coderay', '~> 1.0.0'
      s.add_development_dependency 'rouge', '~> 1.8'
      s.add_development_dependency 'stringex', '~> 1.5.1'
      s.add_development_dependency 'prawn', '~> 2.0'
      s.add_development_dependency 'prawn-table', '~> 0.2.2'
      s.add_development_dependency 'ritex', '~> 1.0'
      s.add_development_dependency 'itextomml', '~> 1.5'

      #### Documentation

      s.has_rdoc = true
      s.rdoc_options = ['--main', 'lib/kramdown/document.rb']

      #### Author and project details

      s.author = 'Thomas Leitner'
      s.email = 't_leitner@gmx.at'
      s.homepage = "http://kramdown.gettalong.org"
    end


    task :gemspec => [ 'CONTRIBUTERS', 'VERSION', 'man/man1/kramdown.1'] do
      print "Generating Gemspec\n"
      contents = spec.to_ruby
      File.open("kramdown.gemspec", 'w+') {|f| f.puts(contents)}
    end

    Gem::PackageTask.new(spec) do |pkg|
      pkg.need_zip = true
      pkg.need_tar = true
    end

  end

  if defined?(Webgen) && defined?(Gem) && defined?(Rake::RDocTask)
    desc 'Release Kramdown version ' + Kramdown::VERSION
    task :release => [:clobber, :package, :publish_files, :publish_website]
  end

  if defined?(Gem)
    desc "Upload the release to Rubygems"
    task :publish_files => [:package] do
      sh "gem push pkg/kramdown-#{Kramdown::VERSION}.gem"
      puts 'done'
    end
  end

  desc "Upload the website"
  task :publish_website => ['doc'] do
    puts "Transfer manually!!!"
    # sh "rsync -avc --delete --exclude 'MathJax' --exclude 'robots.txt'  htmldoc/ gettalong@rubyforge.org:/var/www/gforge-projects/kramdown/"
  end


  if defined? Rcov
    Rcov::RcovTask.new do |rcov|
      rcov.libs << 'test'
    end
  end

  CODING_LINE = "# -*- coding: utf-8 -*-\n"
  COPYRIGHT=<<EOF
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#
EOF

  desc "Insert/Update copyright notice"
  task :update_copyright do
    inserted = false
    Dir["lib/**/*.rb", "test/**/*.rb"].each do |file|
      if !File.read(file).start_with?(CODING_LINE + COPYRIGHT)
        inserted = true
        puts "Updating file #{file}"
        old = File.read(file)
        if !old.gsub!(/\A#{Regexp.escape(CODING_LINE)}#\n#--.*?\n#\+\+\n#\n/m, CODING_LINE + COPYRIGHT)
          old.gsub!(/\A(#{Regexp.escape(CODING_LINE)})?/, CODING_LINE + COPYRIGHT + "\n")
        end
        File.open(file, 'w+') {|f| f.puts(old)}
      end
    end
    puts "Look through the above mentioned files and correct all problems" if inserted
  end

end

task :gemspec => ['dev:gemspec']

task :clobber => ['dev:clobber']
