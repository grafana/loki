# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser/kramdown/escaped_chars'

module Kramdown
  module Parser
    class Kramdown

      # Normalize the link identifier.
      def normalize_link_id(id)
        id.gsub(/[\s]+/, ' ').downcase
      end

      LINK_DEFINITION_START = /^#{OPT_SPACE}\[([^\n\]]+)\]:[ \t]*(?:<(.*?)>|([^\n]*?\S[^\n]*?))(?:(?:[ \t]*?\n|[ \t]+?)[ \t]*?(["'])(.+?)\4)?[ \t]*?\n/

      # Parse the link definition at the current location.
      def parse_link_definition
        return false if @src[3].to_s =~ /[ \t]+["']/
        @src.pos += @src.matched_size
        link_id, link_url, link_title = normalize_link_id(@src[1]), @src[2] || @src[3], @src[5]
        warning("Duplicate link ID '#{link_id}' on line #{@src.current_line_number} - overwriting") if @link_defs[link_id]
        @tree.children << new_block_el(:eob, :link_def)
        @link_defs[link_id] = [link_url, link_title, @tree.children.last]
        true
      end
      define_parser(:link_definition, LINK_DEFINITION_START)


      # This helper methods adds the approriate attributes to the element +el+ of type +a+ or +img+
      # and the element itself to the @tree.
      def add_link(el, href, title, alt_text = nil, ial = nil)
        el.options[:ial] = ial
        update_attr_with_ial(el.attr, ial) if ial
        if el.type == :a
          el.attr['href'] = href
        else
          el.attr['src'] = href
          el.attr['alt'] = alt_text
          el.children.clear
        end
        el.attr['title'] = title if title
        @tree.children << el
      end

      LINK_BRACKET_STOP_RE = /(\])|!?\[/
      LINK_PAREN_STOP_RE = /(\()|(\))|\s(?=['"])/
      LINK_INLINE_ID_RE = /\s*?\[([^\]]+)?\]/
      LINK_INLINE_TITLE_RE = /\s*?(["'])(.+?)\1\s*?\)/m
      LINK_START = /!?\[(?=[^^])/

      # Parse the link at the current scanner position. This method is used to parse normal links as
      # well as image links.
      def parse_link
        start_line_number = @src.current_line_number
        result = @src.scan(LINK_START)
        cur_pos = @src.pos
        saved_pos = @src.save_pos

        link_type = (result =~ /^!/ ? :img : :a)

        # no nested links allowed
        if link_type == :a && (@tree.type == :img || @tree.type == :a || @stack.any? {|t,s| t && (t.type == :img || t.type == :a)})
          add_text(result)
          return
        end
        el = Element.new(link_type, nil, nil, :location => start_line_number)

        count = 1
        found = parse_spans(el, LINK_BRACKET_STOP_RE) do
          count = count + (@src[1] ? -1 : 1)
          count - el.children.select {|c| c.type == :img}.size == 0
        end
        unless found
          @src.revert_pos(saved_pos)
          add_text(result)
          return
        end
        alt_text = extract_string(cur_pos...@src.pos, @src).gsub(ESCAPED_CHARS, '\1')
        @src.scan(LINK_BRACKET_STOP_RE)

        # reference style link or no link url
        if @src.scan(LINK_INLINE_ID_RE) || !@src.check(/\(/)
          link_id = normalize_link_id(@src[1] || alt_text)
          if @link_defs.has_key?(link_id)
            add_link(el, @link_defs[link_id][0], @link_defs[link_id][1], alt_text,
                     @link_defs[link_id][2] && @link_defs[link_id][2].options[:ial])
          else
            warning("No link definition for link ID '#{link_id}' found on line #{start_line_number}")
            @src.revert_pos(saved_pos)
            add_text(result)
          end
          return
        end

        # link url in parentheses
        if @src.scan(/\(<(.*?)>/)
          link_url = @src[1]
          if @src.scan(/\)/)
            add_link(el, link_url, nil, alt_text)
            return
          end
        else
          link_url = ''
          nr_of_brackets = 0
          while temp = @src.scan_until(LINK_PAREN_STOP_RE)
            link_url << temp
            if @src[2]
              nr_of_brackets -= 1
              break if nr_of_brackets == 0
            elsif @src[1]
              nr_of_brackets += 1
            else
              break
            end
          end
          link_url = link_url[1..-2]
          link_url.strip!

          if nr_of_brackets == 0
            add_link(el, link_url, nil, alt_text)
            return
          end
        end

        if @src.scan(LINK_INLINE_TITLE_RE)
          add_link(el, link_url, @src[2], alt_text)
        else
          @src.revert_pos(saved_pos)
          add_text(result)
        end
      end
      define_parser(:link, LINK_START, '!?\[')

    end
  end
end
