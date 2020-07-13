# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/kramdown/block_boundary'

module Kramdown
  module Parser
    class Kramdown

      HEADER_ID=/(?:[ \t]+\{#([A-Za-z][\w:-]*)\})?/
      SETEXT_HEADER_START = /^(#{OPT_SPACE}[^ \t].*?)#{HEADER_ID}[ \t]*?\n(-|=)+\s*?\n/

      # Parse the Setext header at the current location.
      def parse_setext_header
        return false if !after_block_boundary?

        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        text, id, level = @src[1], @src[2], @src[3]
        text.strip!
        el = new_block_el(:header, nil, nil, :level => (level == '-' ? 2 : 1), :raw_text => text, :location => start_line_number)
        add_text(text, el)
        el.attr['id'] = id if id
        @tree.children << el
        true
      end
      define_parser(:setext_header, SETEXT_HEADER_START)


      ATX_HEADER_START = /^\#{1,6}/
      ATX_HEADER_MATCH = /^(\#{1,6})(.+?(?:\\#)?)\s*?#*#{HEADER_ID}\s*?\n/

      # Parse the Atx header at the current location.
      def parse_atx_header
        return false if !after_block_boundary?

        start_line_number = @src.current_line_number
        @src.check(ATX_HEADER_MATCH)
        level, text, id = @src[1], @src[2].to_s.strip, @src[3]
        return false if text.empty?

        @src.pos += @src.matched_size
        el = new_block_el(:header, nil, nil, :level => level.length, :raw_text => text, :location => start_line_number)
        add_text(text, el)
        el.attr['id'] = id if id
        @tree.children << el
        true
      end
      define_parser(:atx_header, ATX_HEADER_START)

    end
  end
end
