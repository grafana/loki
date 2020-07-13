# frozen_string_literal: true

require "bundler/gem_tasks"
require "rake/testtask"

desc "Run all the tests"
task default: :test

desc "Install gem dependencies"
task :deps do
  require 'rubygems'
  spec = Gem::Specification.load('rack.gemspec')
  spec.dependencies.each do |dep|
    reqs = dep.requirements_list
    reqs = (["-v"] * reqs.size).zip(reqs).flatten
    # Use system over sh, because we want to ignore errors!
    system Gem.ruby, "-S", "gem", "install", '--conservative', dep.name, *reqs
  end
end

desc "Make an archive as .tar.gz"
task dist: %w[chmod changelog spec rdoc] do
  sh "git archive --format=tar --prefix=#{release}/ HEAD^{tree} >#{release}.tar"
  sh "pax -waf #{release}.tar -s ':^:#{release}/:' SPEC.rdoc ChangeLog doc rack.gemspec"
  sh "gzip -f -9 #{release}.tar"
end

desc "Make an official release"
task :officialrelease do
  puts "Official build for #{release}..."
  sh "rm -rf stage"
  sh "git clone --shared . stage"
  sh "cd stage && rake officialrelease_really"
  sh "mv stage/#{release}.tar.gz stage/#{release}.gem ."
end

task officialrelease_really: %w[spec dist gem] do
  sh "shasum #{release}.tar.gz #{release}.gem"
end

def release
  "rack-" + File.read('lib/rack/version.rb')[/RELEASE += +([\"\'])([\d][\w\.]+)\1/, 2]
end

desc "Make binaries executable"
task :chmod do
  Dir["bin/*"].each { |binary| File.chmod(0755, binary) }
  Dir["test/cgi/test*"].each { |binary| File.chmod(0755, binary) }
end

desc "Generate a ChangeLog"
task changelog: "ChangeLog"

file '.git/index'
file "ChangeLog" => '.git/index' do
  File.open("ChangeLog", "w") { |out|
    log = `git log -z`
    log.force_encoding(Encoding::BINARY)
    log.split("\0").map { |chunk|
      author = chunk[/Author: (.*)/, 1].strip
      date = chunk[/Date: (.*)/, 1].strip
      desc, detail = $'.strip.split("\n", 2)
      detail ||= ""
      detail = detail.gsub(/.*darcs-hash:.*/, '')
      detail.rstrip!
      out.puts "#{date}  #{author}"
      out.puts "  * #{desc.strip}"
      out.puts detail  unless detail.empty?
      out.puts
    }
  }
end

desc "Generate Rack Specification"
task spec: "SPEC.rdoc"

file 'lib/rack/lint.rb'
file "SPEC.rdoc" => 'lib/rack/lint.rb' do
  File.open("SPEC.rdoc", "wb") { |file|
    IO.foreach("lib/rack/lint.rb") { |line|
      if line =~ /^\s*## ?(.*)/
        file.puts $1
      end
    }
  }
end

Rake::TestTask.new("test:regular") do |t|
  t.libs << "test"
  t.test_files = FileList["test/**/*_test.rb", "test/**/spec_*.rb", "test/gemloader.rb"]
  t.warning = false
  t.verbose = true
end

desc "Run tests with coverage"
task "test_cov" do
  ENV['COVERAGE'] = '1'
  Rake::Task['test:regular'].invoke
end

desc "Run all the fast + platform agnostic tests"
task test: %w[spec test:regular]

desc "Run all the tests we run on CI"
task ci: :test

task gem: :spec do
  sh "gem build rack.gemspec"
end

task doc: :rdoc

desc "Generate RDoc documentation"
task rdoc: %w[changelog spec] do
  sh(*%w{rdoc --line-numbers --main README.rdoc
              --title 'Rack\ Documentation' --charset utf-8 -U -o doc} +
              %w{README.rdoc KNOWN-ISSUES SPEC.rdoc ChangeLog} +
              `git ls-files lib/\*\*/\*.rb`.strip.split)
  cp "contrib/rdoc.css", "doc/rdoc.css"
end

task pushdoc: :rdoc do
  sh "rsync -avz doc/ rack.rubyforge.org:/var/www/gforge-projects/rack/doc/"
end

task pushsite: :pushdoc do
  sh "cd site && git gc"
  sh "rsync -avz site/ rack.rubyforge.org:/var/www/gforge-projects/rack/"
  sh "cd site && git push"
end
