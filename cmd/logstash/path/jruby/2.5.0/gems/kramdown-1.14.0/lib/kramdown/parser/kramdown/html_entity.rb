# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/html'

module Kramdown
  module Parser
    class Kramdown

      # Parse the HTML entity at the current location.
      def parse_html_entity
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        begin
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity(@src[1] || (@src[2] && @src[2].to_i) || @src[3].hex),
                                        nil, :original => @src.matched, :location => start_line_number)
        rescue ::Kramdown::Error
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity('amp'),
                                        nil, :location => start_line_number)
          add_text(@src.matched[1..-1])
        end
      end
      define_parser(:html_entity, Kramdown::Parser::Html::Constants::HTML_ENTITY_RE, '&')

    end
  end
end
