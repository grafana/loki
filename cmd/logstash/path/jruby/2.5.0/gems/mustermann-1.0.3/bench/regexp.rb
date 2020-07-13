require 'benchmark'

puts " atomic vs normal segments ".center(52, '=')

types = {
  normal: /\A\/(?:a|%61)\/(?<b>[^\/\?#]+)(?:\/(?<c>[^\/\?#]+))?\Z/,
  atomic: /\A\/(?:a|%61)\/(?<b>(?>[^\/\?#]+))(?:\/(?<c>(?>[^\/\?#]+)))?\Z/
}

Benchmark.bmbm do |x|
  types.each do |name, regexp|
    string = "/a/" << ?a * 10000 << "/" << ?a * 5000
    fail unless regexp.match(string)
    string << "/"
    fail if regexp.match(string)

    x.report name.to_s do
      100.times { regexp.match(string) }
    end
  end
end