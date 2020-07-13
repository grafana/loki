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

      BLOCK_MATH_START = /^#{OPT_SPACE}(\\)?\$\$(.*?)\$\$(\s*?\n)?/m

      # Parse the math block at the current location.
      def parse_block_math
        start_line_number = @src.current_line_number
        if !after_block_boundary?
          return false
        elsif @src[1]
          @src.scan(/^#{OPT_SPACE}\\/) if @src[3]
          return false
        end

        saved_pos = @src.save_pos
        @src.pos += @src.matched_size
        data = @src[2].strip
        if before_block_boundary?
          @tree.children << new_block_el(:math, data, nil, :category => :block, :location => start_line_number)
          true
        else
          @src.revert_pos(saved_pos)
          false
        end
      end
      define_parser(:block_math, BLOCK_MATH_START)


      INLINE_MATH_START = /\$\$(.*?)\$\$/m

      # Parse the inline math at the current location.
      def parse_inline_math
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        @tree.children << Element.new(:math, @src[1].strip, nil, :category => :span, :location => start_line_number)
      end
      define_parser(:inline_math, INLINE_MATH_START, '\$')

    end
  end
end
