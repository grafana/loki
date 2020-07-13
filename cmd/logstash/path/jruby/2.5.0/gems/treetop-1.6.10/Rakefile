require 'bundler/gem_tasks'
require 'rspec/core/rake_task'
require File.expand_path("../lib/treetop/version", __FILE__)

task :default => :spec
RSpec::Core::RakeTask.new do |t|
  t.pattern = 'spec/**/*spec.rb'
  # t.libs << 'spec' # @todo not sure what this did in the original rspec 1.3
end

task :spec => 'lib/treetop/compiler/metagrammar.treetop'
file 'lib/treetop/compiler/metagrammar.treetop' do |t|
  unless $bootstrapped_gen_1_metagrammar
    load File.expand_path('../lib/treetop/bootstrap_gen_1_metagrammar.rb', __FILE__)
  end

  Treetop::Compiler::GrammarCompiler.new.compile(METAGRAMMAR_PATH)
end

task :rebuild do
  $:.unshift "lib"
  require './lib/treetop'
  load File.expand_path('../lib/treetop/compiler/metagrammar.rb', __FILE__)
  Treetop::Compiler::GrammarCompiler.new.compile('lib/treetop/compiler/metagrammar.treetop')
end

task :version do
  puts 'Ruby is '+RUBY_VERSION
  puts 'Treetop is '+Treetop::VERSION::STRING
end

desc 'Generate website files'
task :website_generate do
  `cd doc; ruby ./site.rb`
end

desc 'Upload website files'
task :website_upload do
  # The website is now done using gh-pages
  system <<-END
    git checkout gh-pages
    cp website/*.html .
    git add *.html
    git commit -m"Website update `date`"
    git push
    git checkout master
  END
end

desc 'Generate and upload website files'
task :website => [:website_generate, :website_upload]
