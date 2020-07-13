$:.unshift File.expand_path('../lib', __dir__)

require 'benchmark'
require 'mustermann'
require 'mustermann/regexp_based'
require 'addressable/template'


Mustermann.register(:regexp, Class.new(Mustermann::RegexpBased) {
  def compile(**options)
    Regexp.new(@string)
  end
}, load: false)

Mustermann.register(:addressable, Class.new(Mustermann::RegexpBased) {
  def compile(**options)
    Addressable::Template.new(@string)
  end
}, load: false)

list = [
  [:sinatra,     '/*/:name'                                ],
  [:rails,       '/*prefix/:name'                          ],
  [:simple,      '/*/:name'                                ],
  [:template,    '{/prefix*}/{name}'                       ],
  [:regexp,      '\A\/(?<splat>.*?)\/(?<name>[^\/\?#]+)\Z' ],
  [:addressable, '{/prefix*}/{name}'                       ]
]

def self.assert(value)
  fail unless value
end

string = '/a/b/c/d'
name   = 'd'

GC.disable

puts "Compilation:"
Benchmark.bmbm do |x|
  list.each do |type, pattern|
    x.report(type) { 1_000.times { Mustermann.new(pattern, type: type) } }
  end
end

puts "", "Matching with two captures (one splat, one normal):"
Benchmark.bmbm do |x|
  list.each do |type, pattern|
    pattern = Mustermann.new(pattern, type: type)
    x.report type do
      10_000.times do
        match = pattern.match(string)
        assert match[:name] == name
      end
    end
  end
end