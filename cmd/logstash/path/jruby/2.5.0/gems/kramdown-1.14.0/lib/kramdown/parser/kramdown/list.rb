# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/kramdown/blank_line'
require 'kramdown/parser/kramdown/eob'
require 'kramdown/parser/kramdown/horizontal_rule'
require 'kramdown/parser/kramdown/extensions'

module Kramdown
  module Parser
    class Kramdown

      LIST_ITEM_IAL = /^\s*(?:\{:(?!(?:#{ALD_ID_NAME})?:|\/)(#{ALD_ANY_CHARS}+)\})\s*/
      LIST_ITEM_IAL_CHECK = /^#{LIST_ITEM_IAL}?\s*\n/

      PARSE_FIRST_LIST_LINE_REGEXP_CACHE = Hash.new do |h, indentation|
        indent_re = /^ {#{indentation}}/
        content_re = /^(?:(?:\t| {4}){#{indentation / 4}} {#{indentation % 4}}|(?:\t| {4}){#{indentation / 4 + 1}}).*\S.*\n/
        lazy_re = /(?!^ {0,#{[indentation, 3].min}}(?:#{IAL_BLOCK}|#{LAZY_END_HTML_STOP}|#{LAZY_END_HTML_START})).*\S.*\n/

        h[indentation] = [content_re, lazy_re, indent_re]
      end

      # Used for parsing the first line of a list item or a definition, i.e. the line with list item
      # marker or the definition marker.
      def parse_first_list_line(indentation, content)
        if content =~ self.class::LIST_ITEM_IAL_CHECK
          indentation = 4
        else
          while content =~ /^ *\t/
            temp = content.scan(/^ */).first.length + indentation
            content.sub!(/^( *)(\t+)/) {$1 << " "*(4 - (temp % 4) + ($2.length - 1)*4)}
          end
          indentation += content[/^ */].length
        end
        content.sub!(/^\s*/, '')

        [content, indentation, *PARSE_FIRST_LIST_LINE_REGEXP_CACHE[indentation]]
      end


      LIST_START_UL = /^(#{OPT_SPACE}[+*-])([\t| ].*?\n)/
      LIST_START_OL = /^(#{OPT_SPACE}\d+\.)([\t| ].*?\n)/
      LIST_START = /#{LIST_START_UL}|#{LIST_START_OL}/

      # Parse the ordered or unordered list at the current location.
      def parse_list
        start_line_number = @src.current_line_number
        type, list_start_re = (@src.check(LIST_START_UL) ? [:ul, LIST_START_UL] : [:ol, LIST_START_OL])
        list = new_block_el(type, nil, nil, :location => start_line_number)

        item = nil
        content_re, lazy_re, indent_re = nil
        eob_found = false
        nested_list_found = false
        last_is_blank = false
        while !@src.eos?
          start_line_number = @src.current_line_number
          if last_is_blank && @src.check(HR_START)
            break
          elsif @src.scan(EOB_MARKER)
            eob_found = true
            break
          elsif @src.scan(list_start_re)
            item = Element.new(:li, nil, nil, :location => start_line_number)
            item.value, indentation, content_re, lazy_re, indent_re = parse_first_list_line(@src[1].length, @src[2])
            list.children << item

            item.value.sub!(self.class::LIST_ITEM_IAL) do |match|
              parse_attribute_list($1, item.options[:ial] ||= {})
              ''
            end

            list_start_re = (type == :ul ? /^( {0,#{[3, indentation - 1].min}}[+*-])([\t| ].*?\n)/ :
                             /^( {0,#{[3, indentation - 1].min}}\d+\.)([\t| ].*?\n)/)
            nested_list_found = (item.value =~ LIST_START)
            last_is_blank = false
            item.value = [item.value]
          elsif (result = @src.scan(content_re)) || (!last_is_blank && (result = @src.scan(lazy_re)))
            result.sub!(/^(\t+)/) { " " * 4 * $1.length }
            indentation_found = result.sub!(indent_re, '')
            if !nested_list_found && indentation_found && result =~ LIST_START
              item.value << ''
              nested_list_found = true
            elsif nested_list_found && !indentation_found && result =~ LIST_START
              result = " " * (indentation + 4) << result
            end
            item.value.last << result
            last_is_blank = false
          elsif result = @src.scan(BLANK_LINE)
            nested_list_found = true
            last_is_blank = true
            item.value.last << result
          else
            break
          end
        end

        @tree.children << list

        last = nil
        list.children.each do |it|
          temp = Element.new(:temp, nil, nil, :location => it.options[:location])

          env = save_env
          location = it.options[:location]
          it.value.each do |val|
            @src = ::Kramdown::Utils::StringScanner.new(val, location)
            parse_blocks(temp)
            location = @src.current_line_number
          end
          restore_env(env)

          it.children = temp.children
          it.value = nil
          next if it.children.size == 0

          # Handle the case where an EOB marker is inserted by a block IAL for the first paragraph
          it.children.delete_at(1) if it.children.first.type == :p &&
            it.children.length >= 2 && it.children[1].type == :eob && it.children.first.options[:ial]

          if it.children.first.type == :p &&
              (it.children.length < 2 || it.children[1].type != :blank ||
               (it == list.children.last && it.children.length == 2 && !eob_found)) &&
              (list.children.last != it || list.children.size == 1 ||
               list.children[0..-2].any? {|cit| !cit.children.first || cit.children.first.type != :p || cit.children.first.options[:transparent]})
            it.children.first.children.first.value << "\n" if it.children.size > 1 && it.children[1].type != :blank
            it.children.first.options[:transparent] = true
          end

          if it.children.last.type == :blank
            last = it.children.pop
          else
            last = nil
          end
        end

        @tree.children << last if !last.nil? && !eob_found

        true
      end
      define_parser(:list, LIST_START)


      DEFINITION_LIST_START = /^(#{OPT_SPACE}:)([\t| ].*?\n)/

      # Parse the ordered or unordered list at the current location.
      def parse_definition_list
        children = @tree.children
        if !children.last || (children.length == 1 && children.last.type != :p ) ||
            (children.length >= 2 && children[-1].type != :p && (children[-1].type != :blank || children[-1].value != "\n" || children[-2].type != :p))
          return false
        end

        first_as_para = false
        deflist = new_block_el(:dl)
        para = @tree.children.pop
        if para.type == :blank
          para = @tree.children.pop
          first_as_para = true
        end
        deflist.options[:location] = para.options[:location] # take location from preceding para which is the first definition term
        para.children.first.value.split(/\n/).each do |term|
          el = Element.new(:dt, nil, nil, :location => @src.current_line_number)
          term.sub!(self.class::LIST_ITEM_IAL) do
            parse_attribute_list($1, el.options[:ial] ||= {})
            ''
          end
          el.options[:raw_text] = term
          el.children << Element.new(:raw_text, term)
          deflist.children << el
        end
        deflist.options[:ial] = para.options[:ial]

        item = nil
        content_re, lazy_re, indent_re = nil
        def_start_re = DEFINITION_LIST_START
        last_is_blank = false
        while !@src.eos?
          start_line_number = @src.current_line_number
          if @src.scan(def_start_re)
            item = Element.new(:dd, nil, nil, :location => start_line_number)
            item.options[:first_as_para] = first_as_para
            item.value, indentation, content_re, lazy_re, indent_re = parse_first_list_line(@src[1].length, @src[2])
            deflist.children << item

            item.value.sub!(self.class::LIST_ITEM_IAL) do |match|
              parse_attribute_list($1, item.options[:ial] ||= {})
              ''
            end

            def_start_re = /^( {0,#{[3, indentation - 1].min}}:)([\t| ].*?\n)/
            first_as_para = false
            last_is_blank = false
          elsif @src.check(EOB_MARKER)
            break
          elsif (result = @src.scan(content_re)) || (!last_is_blank && (result = @src.scan(lazy_re)))
            result.sub!(/^(\t+)/) { " "*($1 ? 4*$1.length : 0) }
            result.sub!(indent_re, '')
            item.value << result
            first_as_para = false
            last_is_blank = false
          elsif result = @src.scan(BLANK_LINE)
            first_as_para = true
            item.value << result
            last_is_blank = true
          else
            break
          end
        end

        last = nil
        deflist.children.each do |it|
          next if it.type == :dt

          parse_blocks(it, it.value)
          it.value = nil
          next if it.children.size == 0

          if it.children.last.type == :blank
            last = it.children.pop
          else
            last = nil
          end

          if it.children.first && it.children.first.type == :p && !it.options.delete(:first_as_para)
            it.children.first.children.first.value << "\n" if it.children.size > 1
            it.children.first.options[:transparent] = true
          end
        end

        if @tree.children.length >= 1 && @tree.children.last.type == :dl
          @tree.children[-1].children.concat(deflist.children)
        elsif @tree.children.length >= 2 && @tree.children[-1].type == :blank && @tree.children[-2].type == :dl
          @tree.children.pop
          @tree.children[-1].children.concat(deflist.children)
        else
          @tree.children << deflist
        end

        @tree.children << last if !last.nil?

        true
      end
      define_parser(:definition_list, DEFINITION_LIST_START)

    end
  end
end
