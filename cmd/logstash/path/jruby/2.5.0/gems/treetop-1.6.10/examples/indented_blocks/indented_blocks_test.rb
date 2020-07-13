require 'polyglot'
require 'byebug'
require 'treetop'
require 'indented_blocks'

parser = IndentedBlocksParser.new

input = <<END
def foo
  here is some indented text
    here it's further indented
    and here the same
      but here it's further again
      and some more like that
    before going back to here
      down again
  back twice
and start from the beginning again
  with only a small block this time
END

parse_tree = parser.parse input

p parse_tree
