$:.unshift File.expand_path('../lib', __dir__)

require 'benchmark'
require 'mustermann/template'
require 'addressable/template'

[Mustermann::Template, Addressable::Template].each do |klass|
  puts "", " #{klass} ".center(64, '=')
  Benchmark.bmbm do |x|
    no_capture = klass.new("/simple")
    x.report("no captures, match") { 1_000.times { no_capture.match('/simple') } }
    x.report("no captures, miss") { 1_000.times { no_capture.match('/miss') } }

    simple = klass.new("/{match}")
    x.report("simple, match") { 1_000.times { simple.match('/simple').captures } }
    x.report("simple, miss") { 1_000.times { simple.match('/mi/ss') } }

    explode = klass.new("{/segments*}")
    x.report("explode, match") { 1_000.times { explode.match("/a/b/c").captures } }
    x.report("explode, miss") { 1_000.times { explode.match("/a/b/c.miss") } }

    expand = klass.new("/prefix/{foo}/something/{bar}")
    x.report("expand") { 100.times { expand.expand(foo: 'foo', bar: 'bar').to_s } }
  end
  puts
end
