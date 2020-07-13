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

      if RUBY_VERSION == '1.8.5'
        ACHARS = '\w\x80-\xFF'
      elsif RUBY_VERSION < '1.9.0'
        ACHARS = '\w'
      else
        ACHARS = '[[:alnum:]]_'
      end
      AUTOLINK_START_STR = "<((mailto|https?|ftps?):.+?|[-.#{ACHARS}]+@[-#{ACHARS}]+(?:\.[-#{ACHARS}]+)*\.[a-z]+)>"
      AUTOLINK_START = /#{AUTOLINK_START_STR}/u

      # Parse the autolink at the current location.
      def parse_autolink
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        href = (@src[2].nil? ? "mailto:#{@src[1]}" : @src[1])
        el = Element.new(:a, nil, {'href' => href}, :location => start_line_number)
        add_text(@src[1].sub(/^mailto:/, ''), el)
        @tree.children << el
      end
      define_parser(:autolink, AUTOLINK_START, '<')

    end
  end
end
