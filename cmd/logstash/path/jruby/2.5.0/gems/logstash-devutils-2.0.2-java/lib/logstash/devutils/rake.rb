raise "Only JRuby is supported at this time." unless RUBY_PLATFORM == "java"
load "logstash/devutils/rake/publish.rake"
load "logstash/devutils/rake/vendor.rake"
