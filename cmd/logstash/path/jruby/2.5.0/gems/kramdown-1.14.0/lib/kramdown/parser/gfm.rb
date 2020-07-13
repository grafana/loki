# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#


require 'kramdown/parser'

module Kramdown
  module Parser
    class GFM < Kramdown::Parser::Kramdown

      def initialize(source, options)
        super
        @options[:auto_id_stripping] = true
        @id_counter = Hash.new(-1)

        @span_parsers.delete(:line_break) if @options[:hard_wrap]
        if @options[:gfm_quirks].include?(:paragraph_end)
          atx_header_parser = :atx_header_gfm_quirk
          @paragraph_end = self.class::PARAGRAPH_END_GFM
        else
          atx_header_parser = :atx_header_gfm
          @paragraph_end = self.class::PARAGRAPH_END
        end

        {:codeblock_fenced => :codeblock_fenced_gfm,
          :atx_header => atx_header_parser}.each do |current, replacement|
          i = @block_parsers.index(current)
          @block_parsers.delete(current)
          @block_parsers.insert(i, replacement)
        end

        i = @span_parsers.index(:escaped_chars)
        @span_parsers[i] = :escaped_chars_gfm if i
        @span_parsers << :strikethrough_gfm
      end

      def parse
        super
        update_elements(@root)
      end

      def update_elements(element)
        element.children.map! do |child|
          if child.type == :text && @options[:hard_wrap] && child.value =~ /\n/
            children = []
            lines = child.value.split(/\n/, -1)
            omit_trailing_br = (Kramdown::Element.category(element) == :block && element.children[-1] == child &&
                                lines[-1].empty?)
            lines.each_with_index do |line, index|
              new_element_options = { :location => child.options[:location] + index }

              children << Element.new(:text, (index > 0 ? "\n#{line}" : line), nil, new_element_options)
              children << Element.new(:br, nil, nil, new_element_options) if index < lines.size - 2 ||
                (index == lines.size - 2 && !omit_trailing_br)
            end
            children
          elsif child.type == :html_element
            child
          elsif child.type == :header && @options[:auto_ids] && !child.attr.has_key?('id')
            child.attr['id'] = generate_gfm_header_id(child.options[:raw_text])
            child
          else
            update_elements(child)
            child
          end
        end.flatten!
      end

      # Update the raw text for automatic ID generation.
      def update_raw_text(item)
        raw_text = ''

        append_text = lambda do |child|
          if child.type == :text || child.type == :codespan || child.type ==:math
            raw_text << child.value
          elsif child.type == :entity
            raw_text << child.value.char
          elsif child.type == :smart_quote
            raw_text << ::Kramdown::Utils::Entities.entity(child.value.to_s).char
          elsif child.type == :typographic_sym
            if child.value == :laquo_space
              raw_text << "« "
            elsif child.value == :raquo_space
              raw_text << " »"
            else
              raw_text << ::Kramdown::Utils::Entities.entity(child.value.to_s).char
            end
          else
            child.children.each {|c| append_text.call(c)}
          end
        end

        append_text.call(item)
        item.options[:raw_text] = raw_text
      end

      NON_WORD_RE = (RUBY_VERSION > "1.9" ? /[^\p{Word}\- \t]/ : /[^\w\- \t]/)

      def generate_gfm_header_id(text)
        result = text.downcase
        result.gsub!(NON_WORD_RE, '')
        result.tr!(" \t", '-')
        @id_counter[result] += 1
        result << (@id_counter[result] > 0 ? "-#{@id_counter[result]}" : '')
        @options[:auto_id_prefix] + result
      end

      ATX_HEADER_START = /^\#{1,6}\s/
      define_parser(:atx_header_gfm, ATX_HEADER_START, nil, 'parse_atx_header')
      define_parser(:atx_header_gfm_quirk, ATX_HEADER_START)

      # Copied from kramdown/parser/kramdown/header.rb, removed the first line
      def parse_atx_header_gfm_quirk
        start_line_number = @src.current_line_number
        @src.check(ATX_HEADER_MATCH)
        level, text, id = @src[1], @src[2].to_s.strip, @src[3]
        return false if text.empty?

        @src.pos += @src.matched_size
        el = new_block_el(:header, nil, nil, :level => level.length, :raw_text => text, :location => start_line_number)
        add_text(text, el)
        el.attr['id'] = id if id
        @tree.children << el
        true
      end

      FENCED_CODEBLOCK_START = /^[ ]{0,3}[~`]{3,}/
      FENCED_CODEBLOCK_MATCH = /^[ ]{0,3}(([~`]){3,})\s*?((\S+?)(?:\?\S*)?)?\s*?\n(.*?)^[ ]{0,3}\1\2*\s*?\n/m
      define_parser(:codeblock_fenced_gfm, FENCED_CODEBLOCK_START, nil, 'parse_codeblock_fenced')

      STRIKETHROUGH_DELIM = /~~/
      STRIKETHROUGH_MATCH = /#{STRIKETHROUGH_DELIM}[^\s~](.*?)[^\s~]#{STRIKETHROUGH_DELIM}/m
      define_parser(:strikethrough_gfm, STRIKETHROUGH_MATCH, '~~')

      def parse_strikethrough_gfm
        line_number = @src.current_line_number

        @src.pos += @src.matched_size
        el = Element.new(:html_element, 'del', {}, :category => :span, :line => line_number)
        @tree.children << el

        env = save_env
        reset_env(:src => Kramdown::Utils::StringScanner.new(@src.matched[2..-3], line_number),
                  :text_type => :text)
        parse_spans(el)
        restore_env(env)

        el
      end

      ESCAPED_CHARS_GFM = /\\([\\.*_+`<>()\[\]{}#!:\|"'\$=\-~])/
      define_parser(:escaped_chars_gfm, ESCAPED_CHARS_GFM, '\\\\', :parse_escaped_chars)

      PARAGRAPH_END_GFM = /#{LAZY_END}|#{LIST_START}|#{ATX_HEADER_START}|#{DEFINITION_LIST_START}|#{BLOCKQUOTE_START}|#{FENCED_CODEBLOCK_START}/

      def paragraph_end
        @paragraph_end
      end

    end
  end
end
