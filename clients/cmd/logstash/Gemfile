source 'https://rubygems.org'

gemspec

logstash_path = ENV["LOGSTASH_PATH"] || "./logstash"

if Dir.exist?(logstash_path)
  gem 'logstash-core', :path => "#{logstash_path}/logstash-core"
  gem 'logstash-core-plugin-api', :path => "#{logstash_path}/logstash-core-plugin-api"
else
  raise 'missing logstash vendoring'
end

gem "webmock", "~> 3.8"
