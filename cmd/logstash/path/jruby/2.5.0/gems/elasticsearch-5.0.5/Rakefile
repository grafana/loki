require "bundler/gem_tasks"

desc "Run unit tests"
task :test    => 'test:unit'

# ----- Test tasks ------------------------------------------------------------

require 'rake/testtask'
namespace :test do
  task :ci_reporter do
    ENV['CI_REPORTS'] ||= 'tmp/reports'
    if defined?(RUBY_VERSION) && RUBY_VERSION < '1.9'
      require 'ci/reporter/rake/test_unit'
      Rake::Task['ci:setup:testunit'].invoke
    else
      require 'ci/reporter/rake/minitest'
      Rake::Task['ci:setup:minitest'].invoke
    end
  end

  Rake::TestTask.new(:unit) do |test|
    Rake::Task['test:ci_reporter'].invoke if ENV['CI']
    test.libs << 'lib' << 'test'
    test.test_files = FileList["test/unit/**/*_test.rb"]
    # test.verbose = true
    # test.warning = true
  end

  Rake::TestTask.new(:integration) do |test|
    Rake::Task['test:ci_reporter'].invoke if ENV['CI']
    test.libs << 'lib' << 'test'
    test.test_files = FileList["test/integration/**/*_test.rb"]
  end

  Rake::TestTask.new(:all) do |test|
    Rake::Task['test:ci_reporter'].invoke if ENV['CI']
    test.libs << 'lib' << 'test'
    test.test_files = FileList["test/unit/**/*_test.rb", "test/integration/**/*_test.rb"]
  end

end

# ----- Documentation tasks ---------------------------------------------------

require 'yard'
YARD::Rake::YardocTask.new(:doc) do |t|
  t.options = %w| --embed-mixins --markup=markdown |
end
