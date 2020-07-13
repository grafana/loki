require 'bundler'
require 'ruby-maven'
require 'rake/testtask'

Bundler::GemHelper.install_tasks

Rake::TestTask.new do |t|
  t.pattern = "test/*_test.rb"
end

desc "Run benchmarks"
task :benchmark do
  load 'benchmarking/benchmark_threaded.rb'
end

desc "Pack jar after compiling classes, use this to rebuild the pom.xml"
task :compile do
  RubyMaven.exec('prepare-package')
end

desc "Clean build"
task :clean do
  RubyMaven.exec('clean')
end

task :default => [ :compile ]

require 'rubygems/package_task'
Gem::PackageTask.new( eval File.read( 'jrjackson.gemspec' ) ) do
  desc 'Pack gem'
  task :package => [:compile]
end

