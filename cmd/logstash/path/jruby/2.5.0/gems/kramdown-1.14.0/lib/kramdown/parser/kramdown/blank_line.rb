# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown
  module Parser
    class Kramdown

      BLANK_LINE = /(?>^\s*\n)+/

      # Parse the blank line at the current postition.
      def parse_blank_line
        @src.pos += @src.matched_size
        if @tree.children.last && @tree.children.last.type == :blank
          @tree.children.last.value << @src.matched
        else
          @tree.children << new_block_el(:blank, @src.matched)
        end
        true
      end
      define_parser(:blank_line, BLANK_LINE)

    end
  end
end
