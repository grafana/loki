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
require 'kramdown/parser/kramdown/codeblock'

module Kramdown
  module Parser
    class Kramdown

      FOOTNOTE_DEFINITION_START = /^#{OPT_SPACE}\[\^(#{ALD_ID_NAME})\]:\s*?(.*?\n#{CODEBLOCK_MATCH})/

      # Parse the foot note definition at the current location.
      def parse_footnote_definition
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size

        el = Element.new(:footnote_def, nil, nil, :location => start_line_number)
        parse_blocks(el, @src[2].gsub(INDENT, ''))
        warning("Duplicate footnote name '#{@src[1]}' on line #{start_line_number} - overwriting") if @footnotes[@src[1]]
        @tree.children << new_block_el(:eob, :footnote_def)
        (@footnotes[@src[1]] = {})[:content] = el
        @footnotes[@src[1]][:eob] = @tree.children.last
        true
      end
      define_parser(:footnote_definition, FOOTNOTE_DEFINITION_START)


      FOOTNOTE_MARKER_START = /\[\^(#{ALD_ID_NAME})\]/

      # Parse the footnote marker at the current location.
      def parse_footnote_marker
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        fn_def = @footnotes[@src[1]]
        if fn_def
          if fn_def[:eob]
            update_attr_with_ial(fn_def[:eob].attr, fn_def[:eob].options[:ial] || {})
            fn_def[:attr] = fn_def[:eob].attr
            fn_def[:options] = fn_def[:eob].options
            fn_def.delete(:eob)
          end
          fn_def[:marker] ||= []
          fn_def[:marker].push(Element.new(:footnote, fn_def[:content], fn_def[:attr],
                                           fn_def[:options].merge(:name => @src[1], :location => start_line_number)))
          @tree.children << fn_def[:marker].last
        else
          warning("Footnote definition for '#{@src[1]}' not found on line #{start_line_number}")
          add_text(@src.matched)
        end
      end
      define_parser(:footnote_marker, FOOTNOTE_MARKER_START, '\[')

    end
  end
end
