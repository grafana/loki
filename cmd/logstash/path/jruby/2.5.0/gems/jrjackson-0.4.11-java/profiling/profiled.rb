require 'rubygems'
require 'jruby-prof'
require 'jrjackson_r'

js = %({"one":1,"two":"deux","three":[333.333,66.666]})

result = JRubyProf.profile do
  JrJackson::Json.parse js
end

JRubyProf.print_flat_text result, "flat.txt"
JRubyProf.print_graph_text result, "graph.txt"
JRubyProf.print_graph_html result, "graph.html"
JRubyProf.print_call_tree result, "call_tree.txt"
JRubyProf.print_tree_html result, "call_tree.html"
