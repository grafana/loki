# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/kramdown/extensions'
require 'kramdown/parser/kramdown/blank_line'
require 'kramdown/parser/kramdown/eob'

module Kramdown
  module Parser
    class Kramdown

      BLOCK_BOUNDARY = /#{BLANK_LINE}|#{EOB_MARKER}|#{IAL_BLOCK_START}|\Z/

      # Return +true+ if we are after a block boundary.
      def after_block_boundary?
        !@tree.children.last || @tree.children.last.type == :blank ||
          (@tree.children.last.type == :eob && @tree.children.last.value.nil?) || @block_ial
      end

      # Return +true+ if we are before a block boundary.
      def before_block_boundary?
        @src.check(self.class::BLOCK_BOUNDARY)
      end

    end
  end
end
