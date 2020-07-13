require 'benchmark'
require 'stringio'

require 'kramdown'
require 'bluecloth'
require 'maruku'
require 'maruku/version'
require 'rdiscount'
require 'bluefeather'
require 'redcarpet'

module MaRuKu::Errors
  def tell_user(s)
  end
end


RUNS=20

FILES=['mdsyntax.text', 'mdbasics.text']

puts "Running tests on #{Time.now.strftime("%Y-%m-%d")} under #{RUBY_DESCRIPTION}"

FILES.each do |file|
  data = File.read(File.join(File.dirname(__FILE__), file))
  puts
  puts "Test using file #{file} and #{RUNS} runs"
  results = Benchmark.bmbm do |b|
    b.report("kramdown #{Kramdown::VERSION}") { RUNS.times { Kramdown::Document.new(data).to_html } }
    b.report("Maruku #{MaRuKu::Version}") { RUNS.times { Maruku.new(data, :on_error => :ignore).to_html } }
    b.report("BlueFeather #{BlueFeather::VERSION}") { RUNS.times { BlueFeather.parse(data) } }
    b.report("BlueCloth #{BlueCloth::VERSION}") { RUNS.times { BlueCloth.new(data).to_html } }
    b.report("RDiscount #{RDiscount::VERSION}") { RUNS.times { RDiscount.new(data).to_html } }
    b.report("redcarpet #{Redcarpet::VERSION}") { RUNS.times { Redcarpet::Markdown.new(Redcarpet::Render::HTML).render(data) } }
  end

  puts
  puts "Real time of X divided by real time of kramdown"
  kd = results.shift.real
  %w[Maruku BlueFeather BlueCloth RDiscount redcarpet].each do |name|
    puts name.ljust(19) << (results.shift.real/kd).round(4).to_s
  end
end
