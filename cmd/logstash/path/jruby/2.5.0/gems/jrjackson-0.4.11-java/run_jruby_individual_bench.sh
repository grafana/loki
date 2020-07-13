#!/bin/sh

jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/json-gen-jr-sourced.rb 
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/gson-gen-jr-sourced.rb 
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/raw-gen-jr-sourced.rb 

# jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/gson-jr-sourced.rb
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/json-jr-sourced.rb

jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/string-jr-sourced.rb
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/str-bd-jr-sourced.rb

jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/symbol-jr-sourced.rb
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/sym-bd-jr-sourced.rb

jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/raw-jr-sourced.rb
jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/raw-bd-jr-sourced.rb

jruby -J-Xmn1024m -J-Xms4096m -J-Xmx4096m -S ./benchmarking/individual/sj-jr-sourced.rb

