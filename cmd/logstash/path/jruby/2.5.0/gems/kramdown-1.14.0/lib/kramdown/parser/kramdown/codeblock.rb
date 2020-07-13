# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/kramdown/blank_line'
require 'kramdown/parser/kramdown/extensions'
require 'kramdown/parser/kramdown/eob'
require 'kramdown/parser/kramdown/paragraph'

module Kramdown
  module Parser
    class Kramdown

      CODEBLOCK_START = INDENT
      CODEBLOCK_MATCH = /(?:#{BLANK_LINE}?(?:#{INDENT}[ \t]*\S.*\n)+(?:(?!#{IAL_BLOCK_START}|#{EOB_MARKER}|^#{OPT_SPACE}#{LAZY_END_HTML_STOP}|^#{OPT_SPACE}#{LAZY_END_HTML_START})^[ \t]*\S.*\n)*)*/

      # Parse the indented codeblock at the current location.
      def parse_codeblock
        start_line_number = @src.current_line_number
        data = @src.scan(self.class::CODEBLOCK_MATCH)
        data.gsub!(/\n( {0,3}\S)/, ' \\1')
        data.gsub!(INDENT, '')
        @tree.children << new_block_el(:codeblock, data, nil, :location => start_line_number)
        true
      end
      define_parser(:codeblock, CODEBLOCK_START)


      FENCED_CODEBLOCK_START = /^~{3,}/
      FENCED_CODEBLOCK_MATCH = /^((~){3,})\s*?((\S+?)(?:\?\S*)?)?\s*?\n(.*?)^\1\2*\s*?\n/m

      # Parse the fenced codeblock at the current location.
      def parse_codeblock_fenced
        if @src.check(self.class::FENCED_CODEBLOCK_MATCH)
          start_line_number = @src.current_line_number
          @src.pos += @src.matched_size
          el = new_block_el(:codeblock, @src[5], nil, :location => start_line_number)
          lang = @src[3].to_s.strip
          unless lang.empty?
            el.options[:lang] = lang
            el.attr['class'] = "language-#{@src[4]}"
          end
          @tree.children << el
          true
        else
          false
        end
      end
      define_parser(:codeblock_fenced, FENCED_CODEBLOCK_START)

    end
  end
end
