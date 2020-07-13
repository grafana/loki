require 'bundler/gem_tasks'

### below are copied from jeweler ###

require 'rake'
# Clean up after gem building
require 'rake/clean'
CLEAN.include('pkg/*.gem')
CLOBBER.include('coverage')

require 'rspec/core'
require 'rspec/core/rake_task'
RSpec::Core::RakeTask.new(:spec) do |spec|
	spec.pattern = FileList['spec/**/*_spec.rb']
end

RSpec::Core::RakeTask.new(:rcov) do |spec|
	spec.pattern = 'spec/**/*_spec.rb'
	spec.rcov = true
end

task :default => :spec

begin
	require 'rdoc/task'
	Rake::RDocTask.new do |rdoc|
		rdoc.rdoc_dir = 'rdoc'
		rdoc.title = 'openssl_pkcs8_pure'
		rdoc.main = 'README.md'
		rdoc.rdoc_files.include('README.*')
		rdoc.rdoc_files.include('LICENSE.*')
		rdoc.rdoc_files.include('CHANGELOG.*')
		rdoc.rdoc_files.include('lib/**/*.rb')
	end
rescue Exception #LoadError # Thus rdoc generation is limited to Ruby 1.9.3+...
#Ruby 1.9.2 gives another Exception. I cannot limit to LoadError...
end
