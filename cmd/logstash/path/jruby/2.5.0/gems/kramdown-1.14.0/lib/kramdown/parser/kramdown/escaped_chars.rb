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

      ESCAPED_CHARS = /\\([\\.*_+`<>()\[\]{}#!:|"'\$=-])/

      # Parse the backslash-escaped character at the current location.
      def parse_escaped_chars
        @src.pos += @src.matched_size
        add_text(@src[1])
      end
      define_parser(:escaped_chars, ESCAPED_CHARS, '\\\\')

    end
  end
end
