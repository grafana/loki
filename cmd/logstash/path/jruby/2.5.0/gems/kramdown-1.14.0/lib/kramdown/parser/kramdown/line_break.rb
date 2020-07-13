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

      LINE_BREAK = /(  |\\\\)(?=\n)/

      # Parse the line break at the current location.
      def parse_line_break
        @tree.children << Element.new(:br, nil, nil, :location => @src.current_line_number)
        @src.pos += @src.matched_size
      end
      define_parser(:line_break, LINE_BREAK, '(  |\\\\)(?=\n)')

    end
  end
end
