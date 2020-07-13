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

      TYPOGRAPHIC_SYMS = [['---', :mdash], ['--', :ndash], ['...', :hellip],
                          ['\\<<', '&lt;&lt;'], ['\\>>', '&gt;&gt;'],
                          ['<< ', :laquo_space], [' >>', :raquo_space],
                          ['<<', :laquo], ['>>', :raquo]]
      TYPOGRAPHIC_SYMS_SUBST = Hash[*TYPOGRAPHIC_SYMS.flatten]
      TYPOGRAPHIC_SYMS_RE = /#{TYPOGRAPHIC_SYMS.map {|k,v| Regexp.escape(k)}.join('|')}/

      # Parse the typographic symbols at the current location.
      def parse_typographic_syms
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        val = TYPOGRAPHIC_SYMS_SUBST[@src.matched]
        if val.kind_of?(Symbol)
          @tree.children << Element.new(:typographic_sym, val, nil, :location => start_line_number)
        elsif @src.matched == '\\<<'
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity('lt'),
                                        nil, :location => start_line_number)
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity('lt'),
                                        nil, :location => start_line_number)
        else
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity('gt'),
                                        nil, :location => start_line_number)
          @tree.children << Element.new(:entity, ::Kramdown::Utils::Entities.entity('gt'),
                                        nil, :location => start_line_number)
        end
      end
      define_parser(:typographic_syms, TYPOGRAPHIC_SYMS_RE, '--|\\.\\.\\.|(?:\\\\| )?(?:<<|>>)')

    end
  end
end
