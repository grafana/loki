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

      include Kramdown::Parser::Html::Parser

      # Mapping of markdown attribute value to content model. I.e. :raw when "0", :default when "1"
      # (use default content model for the HTML element), :span when "span", :block when block and
      # for everything else +nil+ is returned.
      HTML_MARKDOWN_ATTR_MAP = {"0" => :raw, "1" => :default, "span" => :span, "block" => :block}

      TRAILING_WHITESPACE = /[ \t]*\n/

      def handle_kramdown_html_tag(el, closed, handle_body)
        if @block_ial
          el.options[:ial] = @block_ial
          @block_ial = nil
        end

        content_model = if @tree.type != :html_element || @tree.options[:content_model] != :raw
                          (@options[:parse_block_html] ? HTML_CONTENT_MODEL[el.value] : :raw)
                        else
                          :raw
                        end
        if val = HTML_MARKDOWN_ATTR_MAP[el.attr.delete('markdown')]
          content_model = (val == :default ? HTML_CONTENT_MODEL[el.value] : val)
        end

        @src.scan(TRAILING_WHITESPACE) if content_model == :block
        el.options[:content_model] = content_model
        el.options[:is_closed] = closed

        if !closed && handle_body
          if content_model == :block
            if !parse_blocks(el)
              warning("Found no end tag for '#{el.value}' (line #{el.options[:location]}) - auto-closing it")
            end
          elsif content_model == :span
            curpos = @src.pos
            if @src.scan_until(/(?=<\/#{el.value}\s*>)/mi)
              add_text(extract_string(curpos...@src.pos, @src), el)
              @src.scan(HTML_TAG_CLOSE_RE)
            else
              add_text(@src.rest, el)
              @src.terminate
              warning("Found no end tag for '#{el.value}' (line #{el.options[:location]}) - auto-closing it")
            end
          else
            parse_raw_html(el, &method(:handle_kramdown_html_tag))
          end
          @src.scan(TRAILING_WHITESPACE) unless (@tree.type == :html_element && @tree.options[:content_model] == :raw)
        end
      end


      HTML_BLOCK_START = /^#{OPT_SPACE}<(#{REXML::Parsers::BaseParser::UNAME_STR}|\?|!--|\/)/

      # Parse the HTML at the current position as block-level HTML.
      def parse_block_html
        line = @src.current_line_number
        if result = @src.scan(HTML_COMMENT_RE)
          @tree.children << Element.new(:xml_comment, result, nil, :category => :block, :location => line)
          @src.scan(TRAILING_WHITESPACE)
          true
        elsif result = @src.scan(HTML_INSTRUCTION_RE)
          @tree.children << Element.new(:xml_pi, result, nil, :category => :block, :location => line)
          @src.scan(TRAILING_WHITESPACE)
          true
        else
          if result = @src.check(/^#{OPT_SPACE}#{HTML_TAG_RE}/) && !HTML_SPAN_ELEMENTS.include?(@src[1].downcase)
            @src.pos += @src.matched_size
            handle_html_start_tag(line, &method(:handle_kramdown_html_tag))
            Kramdown::Parser::Html::ElementConverter.convert(@root, @tree.children.last) if @options[:html_to_native]
            true
          elsif result = @src.check(/^#{OPT_SPACE}#{HTML_TAG_CLOSE_RE}/) && !HTML_SPAN_ELEMENTS.include?(@src[1].downcase)
            name = @src[1].downcase

            if @tree.type == :html_element && @tree.value == name
              @src.pos += @src.matched_size
              throw :stop_block_parsing, :found
            else
              false
            end
          else
            false
          end
        end
      end
      define_parser(:block_html, HTML_BLOCK_START)


      HTML_SPAN_START = /<(#{REXML::Parsers::BaseParser::UNAME_STR}|\?|!--|\/)/

      # Parse the HTML at the current position as span-level HTML.
      def parse_span_html
        line = @src.current_line_number
        if result = @src.scan(HTML_COMMENT_RE)
          @tree.children << Element.new(:xml_comment, result, nil, :category => :span, :location => line)
        elsif result = @src.scan(HTML_INSTRUCTION_RE)
          @tree.children << Element.new(:xml_pi, result, nil, :category => :span, :location => line)
        elsif result = @src.scan(HTML_TAG_CLOSE_RE)
          warning("Found invalidly used HTML closing tag for '#{@src[1]}' on line #{line}")
          add_text(result)
        elsif result = @src.scan(HTML_TAG_RE)
          tag_name = @src[1]
          tag_name.downcase! if HTML_ELEMENT[tag_name.downcase]
          if HTML_BLOCK_ELEMENTS.include?(tag_name)
            warning("Found block HTML tag '#{tag_name}' in span-level text on line #{line}")
            add_text(result)
            return
          end

          attrs = parse_html_attributes(@src[2], line, HTML_ELEMENT[tag_name])
          attrs.each {|name, value| value.gsub!(/\n+/, ' ')}

          do_parsing = (HTML_CONTENT_MODEL[tag_name] == :raw || @tree.options[:content_model] == :raw ? false : @options[:parse_span_html])
          if val = HTML_MARKDOWN_ATTR_MAP[attrs.delete('markdown')]
            if val == :block
              warning("Cannot use block-level parsing in span-level HTML tag (line #{line}) - using default mode")
            elsif val == :span
              do_parsing = true
            elsif val == :default
              do_parsing = HTML_CONTENT_MODEL[tag_name] != :raw
            elsif val == :raw
              do_parsing = false
            end
          end

          el = Element.new(:html_element, tag_name, attrs, :category => :span, :location => line,
                           :content_model => (do_parsing ? :span : :raw), :is_closed => !!@src[4])
          @tree.children << el
          stop_re = /<\/#{Regexp.escape(tag_name)}\s*>/
          stop_re = Regexp.new(stop_re.source, Regexp::IGNORECASE) if HTML_ELEMENT[tag_name]
          if !@src[4] && !HTML_ELEMENTS_WITHOUT_BODY.include?(el.value)
            if parse_spans(el, stop_re, (do_parsing ? nil : [:span_html]))
              @src.scan(stop_re)
            else
              warning("Found no end tag for '#{el.value}' (line #{line}) - auto-closing it")
              add_text(@src.rest, el)
              @src.terminate
            end
          end
          Kramdown::Parser::Html::ElementConverter.convert(@root, el) if @options[:html_to_native]
        else
          add_text(@src.getch)
        end
      end
      define_parser(:span_html, HTML_SPAN_START, '<')

    end
  end
end
