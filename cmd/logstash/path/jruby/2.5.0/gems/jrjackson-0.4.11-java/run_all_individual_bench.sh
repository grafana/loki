#!/bin/sh

chruby-exec ruby-2.2.2 -- ruby benchmarking/individual/json-mri-sourced.rb
chruby-exec ruby-2.2.2 -- ruby benchmarking/individual/oj-mri-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/gson-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/json-jr-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/string-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/str-bd-jr-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/symbol-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/sym-bd-jr-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/raw-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/raw-bd-jr-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/sj-jr-sourced.rb

chruby-exec ruby-2.2.2 -- ruby benchmarking/individual/json-gen-mri-sourced.rb
chruby-exec ruby-2.2.2 -- ruby benchmarking/individual/oj-gen-mri-sourced.rb

chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/json-gen-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/gson-gen-jr-sourced.rb
chruby-exec jruby-9.2.0.0 -- ruby -J-Xmn512m -J-Xms2048m -J-Xmx2048m benchmarking/individual/raw-gen-jr-sourced.rb
