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

module Kramdown
  module Parser
    class Kramdown

      BLOCKQUOTE_START = /^#{OPT_SPACE}> ?/

      # Parse the blockquote at the current location.
      def parse_blockquote
        start_line_number = @src.current_line_number
        result = @src.scan(PARAGRAPH_MATCH)
        while !@src.match?(self.class::LAZY_END)
          result << @src.scan(PARAGRAPH_MATCH)
        end
        result.gsub!(BLOCKQUOTE_START, '')

        el = new_block_el(:blockquote, nil, nil, :location => start_line_number)
        @tree.children << el
        parse_blocks(el, result)
        true
      end
      define_parser(:blockquote, BLOCKQUOTE_START)


    end
  end
end
