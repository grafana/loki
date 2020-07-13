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

      ABBREV_DEFINITION_START = /^#{OPT_SPACE}\*\[(.+?)\]:(.*?)\n/

      # Parse the link definition at the current location.
      def parse_abbrev_definition
        start_line_number = @src.current_line_number
        @src.pos += @src.matched_size
        abbrev_id, abbrev_text = @src[1], @src[2]
        abbrev_text.strip!
        warning("Duplicate abbreviation ID '#{abbrev_id}' on line #{start_line_number} - overwriting") if @root.options[:abbrev_defs][abbrev_id]
        @tree.children << new_block_el(:eob, :abbrev_def)
        @root.options[:abbrev_defs][abbrev_id] = abbrev_text
        @root.options[:abbrev_attr][abbrev_id] = @tree.children.last
        true
      end
      define_parser(:abbrev_definition, ABBREV_DEFINITION_START)

      # Correct abbreviation attributes.
      def correct_abbreviations_attributes
        @root.options[:abbrev_attr].keys.each do |k|
          @root.options[:abbrev_attr][k] = @root.options[:abbrev_attr][k].attr
        end
      end

      # Replace the abbreviation text with elements.
      def replace_abbreviations(el, regexps = nil)
        return if @root.options[:abbrev_defs].empty?
        if !regexps
          sorted_abbrevs = @root.options[:abbrev_defs].keys.sort {|a,b| b.length <=> a.length}
          regexps = [Regexp.union(*sorted_abbrevs.map {|k| /#{Regexp.escape(k)}/})]
          regexps << /(?=(?:\W|^)#{regexps.first}(?!\w))/ # regexp should only match on word boundaries
        end
        el.children.map! do |child|
          if child.type == :text
            if child.value =~ regexps.first
              result = []
              strscan = Kramdown::Utils::StringScanner.new(child.value, child.options[:location])
              text_lineno = strscan.current_line_number
              while temp = strscan.scan_until(regexps.last)
                abbr_lineno = strscan.current_line_number
                abbr = strscan.scan(regexps.first) # begin of line case of abbr with \W char as first one
                if abbr.nil?
                  temp << strscan.scan(/\W|^/)
                  abbr = strscan.scan(regexps.first)
                end
                result << Element.new(:text, temp, nil, :location => text_lineno)
                result << Element.new(:abbreviation, abbr, nil, :location => abbr_lineno)
                text_lineno = strscan.current_line_number
              end
              result << Element.new(:text, strscan.rest, nil, :location => text_lineno)
            else
              child
            end
          else
            replace_abbreviations(child, regexps)
            child
          end
        end.flatten!
      end

    end
  end
end
