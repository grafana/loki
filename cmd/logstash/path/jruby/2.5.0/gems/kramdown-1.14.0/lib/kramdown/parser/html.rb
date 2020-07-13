# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'rexml/parsers/baseparser'
require 'strscan'
require 'kramdown/utils'
require 'kramdown/parser'

module Kramdown

  module Parser

    # Used for parsing a HTML document.
    #
    # The parsing code is in the Parser module that can also be used by other parsers.
    class Html < Base

      # Contains all constants that are used when parsing.
      module Constants

        #:stopdoc:
        # The following regexps are based on the ones used by REXML, with some slight modifications.
        HTML_DOCTYPE_RE = /<!DOCTYPE.*?>/im
        HTML_COMMENT_RE = /<!--(.*?)-->/m
        HTML_INSTRUCTION_RE = /<\?(.*?)\?>/m
        HTML_ATTRIBUTE_RE = /\s*(#{REXML::Parsers::BaseParser::UNAME_STR})(?:\s*=\s*(["'])(.*?)\2)?/m
        HTML_TAG_RE = /<((?>#{REXML::Parsers::BaseParser::UNAME_STR}))\s*((?>\s+#{REXML::Parsers::BaseParser::UNAME_STR}(?:\s*=\s*(["']).*?\3)?)*)\s*(\/)?>/m
        HTML_TAG_CLOSE_RE = /<\/(#{REXML::Parsers::BaseParser::UNAME_STR})\s*>/m
        HTML_ENTITY_RE = /&([\w:][\-\w\.:]*);|&#(\d+);|&\#x([0-9a-fA-F]+);/

        HTML_CONTENT_MODEL_BLOCK = %w{address applet article aside blockquote body
             dd details div dl fieldset figure figcaption footer form header hgroup iframe li main
             map menu nav noscript object section summary td}
        HTML_CONTENT_MODEL_SPAN  = %w{a abbr acronym b bdo big button cite caption del dfn dt em
             h1 h2 h3 h4 h5 h6 i ins label legend optgroup p q rb rbc
             rp rt rtc ruby select small span strong sub sup th tt}
        HTML_CONTENT_MODEL_RAW   = %w{script style math option textarea pre code kbd samp var}
        # The following elements are also parsed as raw since they need child elements that cannot
        # be expressed using kramdown syntax: colgroup table tbody thead tfoot tr ul ol

        HTML_CONTENT_MODEL = Hash.new {|h,k| h[k] = :raw}
        HTML_CONTENT_MODEL_BLOCK.each {|i| HTML_CONTENT_MODEL[i] = :block}
        HTML_CONTENT_MODEL_SPAN.each {|i| HTML_CONTENT_MODEL[i] = :span}
        HTML_CONTENT_MODEL_RAW.each {|i| HTML_CONTENT_MODEL[i] = :raw}

        # Some HTML elements like script belong to both categories (i.e. are valid in block and
        # span HTML) and don't appear therefore!
        # script, textarea
        HTML_SPAN_ELEMENTS = %w{a abbr acronym b big bdo br button cite code del dfn em i img input
                              ins kbd label mark option q rb rbc rp rt rtc ruby samp select small span
                              strong sub sup tt u var}
        HTML_BLOCK_ELEMENTS = %w{address article aside applet body blockquote caption col colgroup dd div dl dt fieldset
                               figcaption footer form h1 h2 h3 h4 h5 h6 header hgroup hr html head iframe legend menu
                               li main map nav ol optgroup p pre section summary table tbody td th thead tfoot tr ul}
        HTML_ELEMENTS_WITHOUT_BODY = %w{area base br col command embed hr img input keygen link meta param source track wbr}

        HTML_ELEMENT = Hash.new(false)
        (HTML_SPAN_ELEMENTS + HTML_BLOCK_ELEMENTS + HTML_ELEMENTS_WITHOUT_BODY +
         HTML_CONTENT_MODEL.keys).each do |a|
          HTML_ELEMENT[a] = true
        end
      end


      # Contains the parsing methods. This module can be mixed into any parser to get HTML parsing
      # functionality. The only thing that must be provided by the class are instance variable
      # @stack for storing the needed state and @src (instance of StringScanner) for the actual
      # parsing.
      module Parser

        include Constants

        # Process the HTML start tag that has already be scanned/checked via @src.
        #
        # Does the common processing steps and then yields to the caller for further processing
        # (first parameter is the created element; the second parameter is +true+ if the HTML
        # element is already closed, ie. contains no body; the third parameter specifies whether the
        # body - and the end tag - need to be handled in case closed=false).
        def handle_html_start_tag(line = nil) # :yields: el, closed, handle_body
          name = @src[1]
          name.downcase! if HTML_ELEMENT[name.downcase]
          closed = !@src[4].nil?
          attrs = parse_html_attributes(@src[2], line, HTML_ELEMENT[name])

          el = Element.new(:html_element, name, attrs, :category => :block)
          el.options[:location] = line if line
          @tree.children << el

          if !closed && HTML_ELEMENTS_WITHOUT_BODY.include?(el.value)
            closed = true
          end
          if name == 'script' || name == 'style'
            handle_raw_html_tag(name)
            yield(el, false, false)
          else
            yield(el, closed, true)
          end
        end

        # Parses the given string for HTML attributes and returns the resulting hash.
        #
        # If the optional +line+ parameter is supplied, it is used in warning messages.
        #
        # If the optional +in_html_tag+ parameter is set to +false+, attributes are not modified to
        # contain only lowercase letters.
        def parse_html_attributes(str, line = nil, in_html_tag = true)
          attrs = Utils::OrderedHash.new
          str.scan(HTML_ATTRIBUTE_RE).each do |attr, sep, val|
            attr.downcase! if in_html_tag
            if attrs.has_key?(attr)
              warning("Duplicate HTML attribute '#{attr}' on line #{line || '?'} - overwriting previous one")
            end
            attrs[attr] = val || ""
          end
          attrs
        end

        # Handle the raw HTML tag at the current position.
        def handle_raw_html_tag(name)
          curpos = @src.pos
          if @src.scan_until(/(?=<\/#{name}\s*>)/mi)
            add_text(extract_string(curpos...@src.pos, @src), @tree.children.last, :raw)
            @src.scan(HTML_TAG_CLOSE_RE)
          else
            add_text(@src.rest, @tree.children.last, :raw)
            @src.terminate
            warning("Found no end tag for '#{name}' - auto-closing it")
          end
        end

        HTML_RAW_START = /(?=<(#{REXML::Parsers::BaseParser::UNAME_STR}|\/|!--|\?))/ # :nodoc:

        # Parse raw HTML from the current source position, storing the found elements in +el+.
        # Parsing continues until one of the following criteria are fulfilled:
        #
        # - The end of the document is reached.
        # - The matching end tag for the element +el+ is found (only used if +el+ is an HTML
        #   element).
        #
        # When an HTML start tag is found, processing is deferred to #handle_html_start_tag,
        # providing the block given to this method.
        def parse_raw_html(el, &block)
          @stack.push(@tree)
          @tree = el

          done = false
          while !@src.eos? && !done
            if result = @src.scan_until(HTML_RAW_START)
              add_text(result, @tree, :text)
              line = @src.current_line_number
              if result = @src.scan(HTML_COMMENT_RE)
                @tree.children << Element.new(:xml_comment, result, nil, :category => :block, :location => line)
              elsif result = @src.scan(HTML_INSTRUCTION_RE)
                @tree.children << Element.new(:xml_pi, result, nil, :category => :block, :location => line)
              elsif @src.scan(HTML_TAG_RE)
                if method(:handle_html_start_tag).arity.abs >= 1
                  handle_html_start_tag(line, &block)
                else
                  handle_html_start_tag(&block) # DEPRECATED: method needs to accept line number in 2.0
                end
              elsif @src.scan(HTML_TAG_CLOSE_RE)
                if @tree.value == (HTML_ELEMENT[@tree.value] ? @src[1].downcase : @src[1])
                  done = true
                else
                  add_text(@src.matched, @tree, :text)
                  warning("Found invalidly used HTML closing tag for '#{@src[1]}' on line #{line} - ignoring it")
                end
              else
                add_text(@src.getch, @tree, :text)
              end
            else
              add_text(@src.rest, @tree, :text)
              @src.terminate
              warning("Found no end tag for '#{@tree.value}' on line #{@tree.options[:location]} - auto-closing it") if @tree.type == :html_element
              done = true
            end
          end

          @tree = @stack.pop
        end

      end


      # Converts HTML elements to native elements if possible.
      class ElementConverter

        # :stopdoc:

        include Constants
        include ::Kramdown::Utils::Entities

        REMOVE_TEXT_CHILDREN =  %w{html head hgroup ol ul dl table colgroup tbody thead tfoot tr select optgroup}
        WRAP_TEXT_CHILDREN = %w{body section nav article aside header footer address div li dd blockquote figure
                                figcaption fieldset form}
        REMOVE_WHITESPACE_CHILDREN = %w{body section nav article aside header footer address
                                        div li dd blockquote figure figcaption td th fieldset form}
        STRIP_WHITESPACE = %w{address article aside blockquote body caption dd div dl dt fieldset figcaption form footer
                              header h1 h2 h3 h4 h5 h6 legend li nav p section td th}
        SIMPLE_ELEMENTS = %w{em strong blockquote hr br img p thead tbody tfoot tr td th ul ol dl li dl dt dd}

        def initialize(root)
          @root = root
        end

        def self.convert(root, el = root)
          new(root).process(el)
        end

        # Convert the element +el+ and its children.
        def process(el, do_conversion = true, preserve_text = false, parent = nil)
          case el.type
          when :xml_comment, :xml_pi
            ptype = if parent.nil?
                      'div'
                    else
                      case parent.type
                      when :html_element then parent.value
                      when :code_span then 'code'
                      when :code_block then 'pre'
                      when :header then 'h1'
                      else parent.type.to_s
                      end
                    end
            el.options.replace({:category => (HTML_CONTENT_MODEL[ptype] == :span ? :span : :block)})
            return
          when :html_element
          when :root
            el.children.each {|c| process(c)}
            remove_whitespace_children(el)
            return
          else return
          end

          mname = "convert_#{el.value}"
          if do_conversion && self.class.method_defined?(mname)
            send(mname, el)
          else
            type = el.value
            remove_text_children(el) if do_conversion && REMOVE_TEXT_CHILDREN.include?(type)

            if do_conversion && SIMPLE_ELEMENTS.include?(type)
              set_basics(el, type.intern)
              process_children(el, do_conversion, preserve_text)
            else
              process_html_element(el, do_conversion, preserve_text)
            end

            if do_conversion
              strip_whitespace(el) if STRIP_WHITESPACE.include?(type)
              remove_whitespace_children(el) if REMOVE_WHITESPACE_CHILDREN.include?(type)
              wrap_text_children(el) if WRAP_TEXT_CHILDREN.include?(type)
            end
          end
        end

        def process_children(el, do_conversion = true, preserve_text = false)
          el.children.map! do |c|
            if c.type == :text
              process_text(c.value, preserve_text || !do_conversion)
            else
              process(c, do_conversion, preserve_text, el)
              c
            end
          end.flatten!
        end

        # Process the HTML text +raw+: compress whitespace (if +preserve+ is +false+) and convert
        # entities in entity elements.
        def process_text(raw, preserve = false)
          raw.gsub!(/\s+/, ' ') unless preserve
          src = Kramdown::Utils::StringScanner.new(raw)
          result = []
          while !src.eos?
            if tmp = src.scan_until(/(?=#{HTML_ENTITY_RE})/)
              result << Element.new(:text, tmp)
              src.scan(HTML_ENTITY_RE)
              val = src[1] || (src[2] && src[2].to_i) || src[3].hex
              result << if %w{lsquo rsquo ldquo rdquo}.include?(val)
                          Element.new(:smart_quote, val.intern)
                        elsif %w{mdash ndash hellip laquo raquo}.include?(val)
                          Element.new(:typographic_sym, val.intern)
                        else
                          begin
                            Element.new(:entity, entity(val), nil, :original => src.matched)
                          rescue ::Kramdown::Error
                            src.pos -= src.matched_size - 1
                            Element.new(:entity, ::Kramdown::Utils::Entities.entity('amp'))
                          end
                        end
            else
              result << Element.new(:text, src.rest)
              src.terminate
            end
          end
          result
        end

        def process_html_element(el, do_conversion = true, preserve_text = false)
          el.options.replace(:category => HTML_SPAN_ELEMENTS.include?(el.value) ? :span : :block,
                             :content_model => (do_conversion ? HTML_CONTENT_MODEL[el.value] : :raw))
          process_children(el, do_conversion, preserve_text)
        end

        def remove_text_children(el)
          el.children.delete_if {|c| c.type == :text}
        end

        def wrap_text_children(el)
          tmp = []
          last_is_p = false
          el.children.each do |c|
            if Element.category(c) != :block || c.type == :text
              if !last_is_p
                tmp << Element.new(:p, nil, nil, :transparent => true)
                last_is_p = true
              end
              tmp.last.children << c
              tmp
            else
              tmp << c
              last_is_p = false
            end
          end
          el.children = tmp
        end

        def strip_whitespace(el)
          return if el.children.empty?
          if el.children.first.type == :text
            el.children.first.value.lstrip!
          end
          if el.children.last.type == :text
            el.children.last.value.rstrip!
          end
        end

        def remove_whitespace_children(el)
          i = -1
          el.children = el.children.reject do |c|
            i += 1
            c.type == :text && c.value.strip.empty? &&
              (i == 0 || i == el.children.length - 1 || (Element.category(el.children[i-1]) == :block &&
                                                         Element.category(el.children[i+1]) == :block))
          end
        end

        def set_basics(el, type, opts = {})
          el.type = type
          el.options.replace(opts)
          el.value = nil
        end

        def extract_text(el, raw)
          raw << el.value.to_s if el.type == :text
          el.children.each {|c| extract_text(c, raw)}
        end

        def convert_textarea(el)
          process_html_element(el, true, true)
        end

        def convert_a(el)
          if el.attr['href']
            set_basics(el, :a)
            process_children(el)
          else
            process_html_element(el, false)
          end
        end

        EMPHASIS_TYPE_MAP = {'em' => :em, 'i' => :em, 'strong' => :strong, 'b' => :strong}
        def convert_em(el)
          text = ''
          extract_text(el, text)
          if text =~ /\A\s/ || text =~ /\s\z/
            process_html_element(el, false)
          else
            set_basics(el, EMPHASIS_TYPE_MAP[el.value])
            process_children(el)
          end
        end
        %w{b strong i}.each do |i|
          alias_method("convert_#{i}".to_sym, :convert_em)
        end

        def convert_h1(el)
          set_basics(el, :header, :level => el.value[1..1].to_i)
          extract_text(el, el.options[:raw_text] = '')
          process_children(el)
        end
        %w{h2 h3 h4 h5 h6}.each do |i|
          alias_method("convert_#{i}".to_sym, :convert_h1)
        end

        def convert_code(el)
          raw = ''
          extract_text(el, raw)
          result = process_text(raw, true)
          begin
            str = result.inject('') do |mem, c|
              if c.type == :text
                mem << c.value
              elsif c.type == :entity
                if [60, 62, 34, 38].include?(c.value.code_point)
                  mem << c.value.code_point.chr
                else
                  mem << c.value.char
                end
              elsif c.type == :smart_quote || c.type == :typographic_sym
                mem << entity(c.value.to_s).char
              else
                raise "Bug - please report"
              end
            end
            result.clear
            result << Element.new(:text, str)
          rescue
          end
          if result.length > 1 || result.first.type != :text
            process_html_element(el, false, true)
          else
            if el.value == 'code'
              set_basics(el, :codespan)
              el.attr['class'].gsub!(/\s+\bhighlighter-\w+\b|\bhighlighter-\w+\b\s*/, '') if el.attr['class']
            else
              set_basics(el, :codeblock)
              if el.children.size == 1 && el.children.first.value == 'code'
                value = (el.children.first.attr['class'] || '').scan(/\blanguage-\S+/).first
                el.attr['class'] = "#{value} #{el.attr['class']}".rstrip if value
              end
            end
            el.value = result.first.value
            el.children.clear
          end
        end
        alias :convert_pre :convert_code

        def convert_table(el)
          if !is_simple_table?(el)
            process_html_element(el, false)
            return
          end
          remove_text_children(el)
          process_children(el)
          set_basics(el, :table)

          calc_alignment = lambda do |c|
            if c.type == :tr
              el.options[:alignment] = c.children.map do |td|
                if td.attr['style']
                  td.attr['style'].slice!(/(?:;\s*)?text-align:\s+(center|left|right)/)
                  td.attr.delete('style') if td.attr['style'].strip.empty?
                  $1 ? $1.to_sym : :default
                else
                  :default
                end
              end
            else
              c.children.each {|cc| calc_alignment.call(cc)}
            end
          end
          calc_alignment.call(el)
          el.children.delete_if {|c| c.type == :html_element}

          change_th_type = lambda do |c|
            if c.type == :th
              c.type = :td
            else
              c.children.each {|cc| change_th_type.call(cc)}
            end
          end
          change_th_type.call(el)

          if el.children.first.type == :tr
            tbody = Element.new(:tbody)
            tbody.children = el.children
            el.children = [tbody]
          end
        end

        def is_simple_table?(el)
          only_phrasing_content = lambda do |c|
            c.children.all? do |cc|
              (cc.type == :text || !HTML_BLOCK_ELEMENTS.include?(cc.value)) && only_phrasing_content.call(cc)
            end
          end
          check_cells = Proc.new do |c|
            if c.value == 'th' || c.value == 'td'
              return false if !only_phrasing_content.call(c)
            else
              c.children.each {|cc| check_cells.call(cc)}
            end
          end
          check_cells.call(el)

          nr_cells = 0
          check_nr_cells = lambda do |t|
            if t.value == 'tr'
              count = t.children.select {|cc| cc.value == 'th' || cc.value == 'td'}.length
              if count != nr_cells
                if nr_cells == 0
                  nr_cells = count
                else
                  nr_cells = -1
                  break
                end
              end
            else
              t.children.each {|cc| check_nr_cells.call(cc)}
            end
          end
          check_nr_cells.call(el)
          return false if nr_cells == -1

          alignment = nil
          check_alignment = Proc.new do |t|
            if t.value == 'tr'
              cur_alignment = t.children.select {|cc| cc.value == 'th' || cc.value == 'td'}.map do |cell|
                md = /text-align:\s+(center|left|right|justify|inherit)/.match(cell.attr['style'].to_s)
                return false if md && (md[1] == 'justify' || md[1] == 'inherit')
                md.nil? ? :default : md[1]
              end
              alignment = cur_alignment if alignment.nil?
              return false if alignment != cur_alignment
            else
              t.children.each {|cc| check_alignment.call(cc)}
            end
          end
          check_alignment.call(el)

          check_rows = lambda do |t, type|
            t.children.all? {|r| (r.value == 'tr' || r.type == :text) && r.children.all? {|c| c.value == type || c.type == :text}}
          end
          check_rows.call(el, 'td') ||
            (el.children.all? do |t|
               t.type == :text || (t.value == 'thead' && check_rows.call(t, 'th')) ||
                 ((t.value == 'tfoot' || t.value == 'tbody') && check_rows.call(t, 'td'))
             end && el.children.any? {|t| t.value == 'tbody'})
        end

        def convert_script(el)
          if !is_math_tag?(el)
            process_html_element(el)
          else
            handle_math_tag(el)
          end
        end

        def is_math_tag?(el)
          el.attr['type'].to_s =~ /\bmath\/tex\b/
        end

        def handle_math_tag(el)
          set_basics(el, :math, :category => (el.attr['type'] =~ /mode=display/ ? :block : :span))
          el.value = el.children.shift.value.sub(/\A(?:%\s*)?<!\[CDATA\[\n?(.*?)(?:\s%)?\]\]>\z/m, '\1')
          el.attr.delete('type')
        end

      end

      include Parser

      # Parse the source string provided on initialization as HTML document.
      def parse
        @stack, @tree = [], @root
        @src = Kramdown::Utils::StringScanner.new(adapt_source(source))

        while true
          if result = @src.scan(/\s*#{HTML_INSTRUCTION_RE}/)
            @tree.children << Element.new(:xml_pi, result.strip, nil, :category => :block)
          elsif result = @src.scan(/\s*#{HTML_DOCTYPE_RE}/)
            # ignore the doctype
          elsif result = @src.scan(/\s*#{HTML_COMMENT_RE}/)
            @tree.children << Element.new(:xml_comment, result.strip, nil, :category => :block)
          else
            break
          end
        end

        tag_handler = lambda do |c, closed, handle_body|
          parse_raw_html(c, &tag_handler) if !closed && handle_body
        end
        parse_raw_html(@tree, &tag_handler)

        ElementConverter.convert(@tree)
      end

    end

  end

end

