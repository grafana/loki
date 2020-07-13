require 'rspec'
RSpec.configure{|config|
	config.color=true
}

if !defined?(RUBY_ENGINE)||RUBY_ENGINE=='ruby'
	require 'simplecov'
	require 'coveralls'
	SimpleCov.formatter = SimpleCov::Formatter::MultiFormatter[
		SimpleCov::Formatter::HTMLFormatter,
		Coveralls::SimpleCov::Formatter
	]
	SimpleCov.start do
		add_filter 'spec'
	end
end

require File.expand_path(File.dirname(__FILE__)+'/../lib/openssl_pkcs8_pure')
