# coding: utf-8
require 'benchmark/ips'
require 'json'
require File.expand_path('lib/jrjackson')

obj = {
  :name => "Fredrick Smith",
  :quantity => 1_000_000,
  :addresses => {
    :address1 => "12 Heather Street, Parnell, Auckland, New Zealand",
    :address2 => "1 Queen Street, CBD, Auckland, New Zealand"
  }
}

json = JSON.dump(obj)

puts "Decode ====================="

puts 'Sleeping'
sleep 45
puts 'Working'

# -JXrunhprof:cpu=samples,depth=30,file=jrjackson.hprof

Benchmark.ips do |x|
  x.config(time: 20, warmup: 20)

  x.report("JrJackson new Ruby") { JrJackson::Ruby.parse(json, nil) }
  # x.report("JrJackson Raw") { JrJackson::Java.parse(json, nil) }
  # x.report("JSON") { JSON.load(json) }

  # x.report("JrJackson") { JrJackson::Raw.parse_sym(json) }

  # x.compare!
end

# puts "Encode ====================="
# Benchmark.ips do |x|
#   x.config(time: 5, warmup: 10)

#   x.report("JrJackson Raw") { JrJackson::Base.generate(obj) }
#   x.report("JrJackson") { JrJackson::Json.dump(obj) }
#   x.report("JSON") { JSON.dump(obj) }

#   x.compare!
# end
