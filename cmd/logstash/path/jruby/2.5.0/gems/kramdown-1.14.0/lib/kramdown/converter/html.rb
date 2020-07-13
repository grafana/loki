# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/parser'
require 'kramdown/converter'
require 'kramdown/utils'

module Kramdown

  module Converter

    # Converts a Kramdown::Document to HTML.
    #
    # You can customize the HTML converter by sub-classing it and overriding the +convert_NAME+
    # methods. Each such method takes the following parameters:
    #
    # [+el+] The element of type +NAME+ to be converted.
    #
    # [+indent+] A number representing the current amount of spaces for indent (only used for
    #            block-level elements).
    #
    # The return value of such a method has to be a string containing the element +el+ formatted as
    # HTML element.
    class Html < Base

      include ::Kramdown::Utils::Html
      include ::Kramdown::Parser::Html::Constants

      # The amount of indentation used when nesting HTML tags.
      attr_accessor :indent

      # Initialize the HTML converter with the given Kramdown document +doc+.
      def initialize(root, options)
        super
        @footnote_counter = @footnote_start = @options[:footnote_nr]
        @footnotes = []
        @footnotes_by_name = {}
        @footnote_location = nil
        @toc = []
        @toc_code = nil
        @indent = 2
        @stack = []
      end

      # The mapping of element type to conversion method.
      DISPATCHER = Hash.new {|h,k| h[k] = "convert_#{k}"}

      # Dispatch the conversion of the element +el+ to a +convert_TYPE+ method using the +type+ of
      # the element.
      def convert(el, indent = -@indent)
        send(DISPATCHER[el.type], el, indent)
      end

      # Return the converted content of the children of +el+ as a string. The parameter +indent+ has
      # to be the amount of indentation used for the element +el+.
      #
      # Pushes +el+ onto the @stack before converting the child elements and pops it from the stack
      # afterwards.
      def inner(el, indent)
        result = ''
        indent += @indent
        @stack.push(el)
        el.children.each do |inner_el|
          result << send(DISPATCHER[inner_el.type], inner_el, indent)
        end
        @stack.pop
        result
      end

      def convert_blank(el, indent)
        "\n"
      end

      def convert_text(el, indent)
        escape_html(el.value, :text)
      end

      def convert_p(el, indent)
        if el.options[:transparent]
          inner(el, indent)
        else
          format_as_block_html(el.type, el.attr, inner(el, indent), indent)
        end
      end

      def convert_codeblock(el, indent)
        attr = el.attr.dup
        lang = extract_code_language!(attr)
        hl_opts = {}
        highlighted_code = highlight_code(el.value, el.options[:lang] || lang, :block, hl_opts)

        if highlighted_code
          add_syntax_highlighter_to_class_attr(attr, lang || hl_opts[:default_lang])
          "#{' '*indent}<div#{html_attributes(attr)}>#{highlighted_code}#{' '*indent}</div>\n"
        else
          result = escape_html(el.value)
          result.chomp!
          if el.attr['class'].to_s =~ /\bshow-whitespaces\b/
            result.gsub!(/(?:(^[ \t]+)|([ \t]+$)|([ \t]+))/) do |m|
              suffix = ($1 ? '-l' : ($2 ? '-r' : ''))
              m.scan(/./).map do |c|
                case c
                when "\t" then "<span class=\"ws-tab#{suffix}\">\t</span>"
                when " " then "<span class=\"ws-space#{suffix}\">&#8901;</span>"
                end
              end.join('')
            end
          end
          code_attr = {}
          code_attr['class'] = "language-#{lang}" if lang
          "#{' '*indent}<pre#{html_attributes(attr)}><code#{html_attributes(code_attr)}>#{result}\n</code></pre>\n"
        end
      end

      def convert_blockquote(el, indent)
        format_as_indented_block_html(el.type, el.attr, inner(el, indent), indent)
      end

      def convert_header(el, indent)
        attr = el.attr.dup
        if @options[:auto_ids] && !attr['id']
          attr['id'] = generate_id(el.options[:raw_text])
        end
        @toc << [el.options[:level], attr['id'], el.children] if attr['id'] && in_toc?(el)
        level = output_header_level(el.options[:level])
        format_as_block_html("h#{level}", attr, inner(el, indent), indent)
      end

      def convert_hr(el, indent)
        "#{' '*indent}<hr#{html_attributes(el.attr)} />\n"
      end

      def convert_ul(el, indent)
        if !@toc_code && (el.options[:ial][:refs].include?('toc') rescue nil)
          @toc_code = [el.type, el.attr, (0..128).to_a.map{|a| rand(36).to_s(36)}.join]
          @toc_code.last
        elsif !@footnote_location && el.options[:ial] && (el.options[:ial][:refs] || []).include?('footnotes')
          @footnote_location = (0..128).to_a.map{|a| rand(36).to_s(36)}.join
        else
          format_as_indented_block_html(el.type, el.attr, inner(el, indent), indent)
        end
      end
      alias :convert_ol :convert_ul

      def convert_dl(el, indent)
        format_as_indented_block_html(el.type, el.attr, inner(el, indent), indent)
      end

      def convert_li(el, indent)
        output = ' '*indent << "<#{el.type}" << html_attributes(el.attr) << ">"
        res = inner(el, indent)
        if el.children.empty? || (el.children.first.type == :p && el.children.first.options[:transparent])
          output << res << (res =~ /\n\Z/ ? ' '*indent : '')
        else
          output << "\n" << res << ' '*indent
        end
        output << "</#{el.type}>\n"
      end
      alias :convert_dd :convert_li

      def convert_dt(el, indent)
        attr = el.attr.dup
        @stack.last.options[:ial][:refs].each do |ref|
          if ref =~ /\Aauto_ids(?:-([\w-]+))?/
            attr['id'] = ($1 ? $1 : '') << basic_generate_id(el.options[:raw_text])
            break
          end
        end if !attr['id'] && @stack.last.options[:ial] && @stack.last.options[:ial][:refs]
        format_as_block_html(el.type, attr, inner(el, indent), indent)
      end

      def convert_html_element(el, indent)
        res = inner(el, indent)
        if el.options[:category] == :span
          "<#{el.value}#{html_attributes(el.attr)}" << (res.empty? && HTML_ELEMENTS_WITHOUT_BODY.include?(el.value) ? " />" : ">#{res}</#{el.value}>")
        else
          output = ''
          output << ' '*indent if @stack.last.type != :html_element || @stack.last.options[:content_model] != :raw
          output << "<#{el.value}#{html_attributes(el.attr)}"
          if el.options[:is_closed] && el.options[:content_model] == :raw
            output << " />"
          elsif !res.empty? && el.options[:content_model] != :block
            output << ">#{res}</#{el.value}>"
          elsif !res.empty?
            output << ">\n#{res.chomp}\n"  << ' '*indent << "</#{el.value}>"
          elsif HTML_ELEMENTS_WITHOUT_BODY.include?(el.value)
            output << " />"
          else
            output << "></#{el.value}>"
          end
          output << "\n" if @stack.last.type != :html_element || @stack.last.options[:content_model] != :raw
          output
        end
      end

      def convert_xml_comment(el, indent)
        if el.options[:category] == :block && (@stack.last.type != :html_element || @stack.last.options[:content_model] != :raw)
          ' '*indent << el.value << "\n"
        else
          el.value
        end
      end
      alias :convert_xml_pi :convert_xml_comment

      def convert_table(el, indent)
        format_as_indented_block_html(el.type, el.attr, inner(el, indent), indent)
      end
      alias :convert_thead :convert_table
      alias :convert_tbody :convert_table
      alias :convert_tfoot :convert_table
      alias :convert_tr  :convert_table

      ENTITY_NBSP = ::Kramdown::Utils::Entities.entity('nbsp') # :nodoc:

      def convert_td(el, indent)
        res = inner(el, indent)
        type = (@stack[-2].type == :thead ? :th : :td)
        attr = el.attr
        alignment = @stack[-3].options[:alignment][@stack.last.children.index(el)]
        if alignment != :default
          attr = el.attr.dup
          attr['style'] = (attr.has_key?('style') ? "#{attr['style']}; ": '') << "text-align: #{alignment}"
        end
        format_as_block_html(type, attr, res.empty? ? entity_to_str(ENTITY_NBSP) : res, indent)
      end

      def convert_comment(el, indent)
        if el.options[:category] == :block
          "#{' '*indent}<!-- #{el.value} -->\n"
        else
          "<!-- #{el.value} -->"
        end
      end

      def convert_br(el, indent)
        "<br />"
      end

      def convert_a(el, indent)
        format_as_span_html(el.type, el.attr, inner(el, indent))
      end

      def convert_img(el, indent)
        "<img#{html_attributes(el.attr)} />"
      end

      def convert_codespan(el, indent)
        attr = el.attr.dup
        lang = extract_code_language(attr)
        hl_opts = {}
        result = highlight_code(el.value, lang, :span, hl_opts)
        if result
          add_syntax_highlighter_to_class_attr(attr, hl_opts[:default_lang])
        else
          result = escape_html(el.value)
        end

        format_as_span_html('code', attr, result)
      end

      def convert_footnote(el, indent)
        repeat = ''
        if (footnote = @footnotes_by_name[el.options[:name]])
          number = footnote[2]
          repeat = ":#{footnote[3] += 1}"
        else
          number = @footnote_counter
          @footnote_counter += 1
          @footnotes << [el.options[:name], el.value, number, 0]
          @footnotes_by_name[el.options[:name]] = @footnotes.last
        end
        "<sup id=\"fnref:#{el.options[:name]}#{repeat}\"><a href=\"#fn:#{el.options[:name]}\" class=\"footnote\">#{number}</a></sup>"
      end

      def convert_raw(el, indent)
        if !el.options[:type] || el.options[:type].empty? || el.options[:type].include?('html')
          el.value + (el.options[:category] == :block ? "\n" : '')
        else
          ''
        end
      end

      def convert_em(el, indent)
        format_as_span_html(el.type, el.attr, inner(el, indent))
      end
      alias :convert_strong :convert_em

      def convert_entity(el, indent)
        entity_to_str(el.value, el.options[:original])
      end

      TYPOGRAPHIC_SYMS = {
        :mdash => [::Kramdown::Utils::Entities.entity('mdash')],
        :ndash => [::Kramdown::Utils::Entities.entity('ndash')],
        :hellip => [::Kramdown::Utils::Entities.entity('hellip')],
        :laquo_space => [::Kramdown::Utils::Entities.entity('laquo'), ::Kramdown::Utils::Entities.entity('nbsp')],
        :raquo_space => [::Kramdown::Utils::Entities.entity('nbsp'), ::Kramdown::Utils::Entities.entity('raquo')],
        :laquo => [::Kramdown::Utils::Entities.entity('laquo')],
        :raquo => [::Kramdown::Utils::Entities.entity('raquo')]
      } # :nodoc:
      def convert_typographic_sym(el, indent)
        TYPOGRAPHIC_SYMS[el.value].map {|e| entity_to_str(e)}.join('')
      end

      def convert_smart_quote(el, indent)
        entity_to_str(smart_quote_entity(el))
      end

      def convert_math(el, indent)
        if (result = format_math(el, :indent => indent))
          result
        else
          attr = el.attr.dup
          (attr['class'] = (attr['class'] || '') << " kdmath").lstrip!
          if el.options[:category] == :block
            format_as_block_html('div', attr, "$$\n#{el.value}\n$$", indent)
          else
            format_as_span_html('span', attr, "$#{el.value}$")
          end
        end
      end

      def convert_abbreviation(el, indent)
        title = @root.options[:abbrev_defs][el.value]
        attr = @root.options[:abbrev_attr][el.value].dup
        attr['title'] = title unless title.empty?
        format_as_span_html("abbr", attr, el.value)
      end

      def convert_root(el, indent)
        result = inner(el, indent)
        if @footnote_location
          result.sub!(/#{@footnote_location}/, footnote_content.gsub(/\\/, "\\\\\\\\"))
        else
          result << footnote_content
        end
        if @toc_code
          toc_tree = generate_toc_tree(@toc, @toc_code[0], @toc_code[1] || {})
          text = if toc_tree.children.size > 0
                   convert(toc_tree, 0)
                 else
                   ''
                 end
          result.sub!(/#{@toc_code.last}/, text.gsub(/\\/, "\\\\\\\\"))
        end
        result
      end

      # Format the given element as span HTML.
      def format_as_span_html(name, attr, body)
        "<#{name}#{html_attributes(attr)}>#{body}</#{name}>"
      end

      # Format the given element as block HTML.
      def format_as_block_html(name, attr, body, indent)
        "#{' '*indent}<#{name}#{html_attributes(attr)}>#{body}</#{name}>\n"
      end

      # Format the given element as block HTML with a newline after the start tag and indentation
      # before the end tag.
      def format_as_indented_block_html(name, attr, body, indent)
        "#{' '*indent}<#{name}#{html_attributes(attr)}>\n#{body}#{' '*indent}</#{name}>\n"
      end

      # Add the syntax highlighter name to the 'class' attribute of the given attribute hash. And
      # overwrites or add a "language-LANG" part using the +lang+ parameter if +lang+ is not nil.
      def add_syntax_highlighter_to_class_attr(attr, lang = nil)
        (attr['class'] = (attr['class'] || '') + " highlighter-#{@options[:syntax_highlighter]}").lstrip!
        attr['class'].sub!(/\blanguage-\S+|(^)/) { "language-#{lang}#{$1 ? ' ' : ''}" } if lang
      end

      # Generate and return an element tree for the table of contents.
      def generate_toc_tree(toc, type, attr)
        sections = Element.new(type, nil, attr)
        sections.attr['id'] ||= 'markdown-toc'
        stack = []
        toc.each do |level, id, children|
          li = Element.new(:li, nil, nil, {:level => level})
          li.children << Element.new(:p, nil, nil, {:transparent => true})
          a = Element.new(:a, nil)
          a.attr['href'] = "##{id}"
          a.attr['id'] = "#{sections.attr['id']}-#{id}"
          a.children.concat(remove_footnotes(Marshal.load(Marshal.dump(children))))
          li.children.last.children << a
          li.children << Element.new(type)

          success = false
          while !success
            if stack.empty?
              sections.children << li
              stack << li
              success = true
            elsif stack.last.options[:level] < li.options[:level]
              stack.last.children.last.children << li
              stack << li
              success = true
            else
              item = stack.pop
              item.children.pop unless item.children.last.children.size > 0
            end
          end
        end
        while !stack.empty?
          item = stack.pop
          item.children.pop unless item.children.last.children.size > 0
        end
        sections
      end

      # Remove all footnotes from the given elements.
      def remove_footnotes(elements)
        elements.delete_if do |c|
          remove_footnotes(c.children)
          c.type == :footnote
        end
      end

      # Obfuscate the +text+ by using HTML entities.
      def obfuscate(text)
        result = ""
        text.each_byte do |b|
          result << (b > 128 ? b.chr : "&#%03d;" % b)
        end
        result.force_encoding(text.encoding) if result.respond_to?(:force_encoding)
        result
      end

      FOOTNOTE_BACKLINK_FMT = "%s<a href=\"#fnref:%s\" class=\"reversefootnote\">%s</a>"

      # Return a HTML ordered list with the footnote content for the used footnotes.
      def footnote_content
        ol = Element.new(:ol)
        ol.attr['start'] = @footnote_start if @footnote_start != 1
        i = 0
        backlink_text = escape_html(@options[:footnote_backlink], :text)
        while i < @footnotes.length
          name, data, _, repeat = *@footnotes[i]
          li = Element.new(:li, nil, {'id' => "fn:#{name}"})
          li.children = Marshal.load(Marshal.dump(data.children))

          if li.children.last.type == :p
            para = li.children.last
            insert_space = true
          else
            li.children << (para = Element.new(:p))
            insert_space = false
          end

          unless @options[:footnote_backlink].empty?
            para.children << Element.new(:raw, FOOTNOTE_BACKLINK_FMT % [insert_space ? '&nbsp;' : '', name, backlink_text])
            (1..repeat).each do |index|
              para.children << Element.new(:raw, FOOTNOTE_BACKLINK_FMT % ["&nbsp;", "#{name}:#{index}", "#{backlink_text}<sup>#{index+1}</sup>"])
            end
          end

          ol.children << Element.new(:raw, convert(li, 4))
          i += 1
        end
        (ol.children.empty? ? '' : format_as_indented_block_html('div', {:class => "footnotes"}, convert(ol, 2), 0))
      end

    end

  end
end
