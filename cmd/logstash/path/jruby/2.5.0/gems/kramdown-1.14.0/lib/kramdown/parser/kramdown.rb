# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'strscan'
require 'stringio'
require 'kramdown/parser'

#TODO: use [[:alpha:]] in all regexp to allow parsing of international values in 1.9.1
#NOTE: use @src.pre_match only before other check/match?/... operations, otherwise the content is changed

module Kramdown

  module Parser

    # Used for parsing a document in kramdown format.
    #
    # If you want to extend the functionality of the parser, you need to do the following:
    #
    # * Create a new subclass
    # * add the needed parser methods
    # * modify the @block_parsers and @span_parsers variables and add the names of your parser
    #   methods
    #
    # Here is a small example for an extended parser class that parses ERB style tags as raw text if
    # they are used as span-level elements (an equivalent block-level parser should probably also be
    # made to handle the block case):
    #
    #   require 'kramdown/parser/kramdown'
    #
    #   class Kramdown::Parser::ERBKramdown < Kramdown::Parser::Kramdown
    #
    #      def initialize(source, options)
    #        super
    #        @span_parsers.unshift(:erb_tags)
    #      end
    #
    #      ERB_TAGS_START = /<%.*?%>/
    #
    #      def parse_erb_tags
    #        @src.pos += @src.matched_size
    #        @tree.children << Element.new(:raw, @src.matched)
    #      end
    #      define_parser(:erb_tags, ERB_TAGS_START, '<%')
    #
    #   end
    #
    # The new parser can be used like this:
    #
    #   require 'kramdown/document'
    #   # require the file with the above parser class
    #
    #   Kramdown::Document.new(input_text, :input => 'ERBKramdown').to_html
    #
    class Kramdown < Base

      include ::Kramdown

      # Create a new Kramdown parser object with the given +options+.
      def initialize(source, options)
        super

        reset_env

        @alds = {}
        @footnotes = {}
        @link_defs = {}
        update_link_definitions(@options[:link_defs])

        @block_parsers = [:blank_line, :codeblock, :codeblock_fenced, :blockquote, :atx_header,
                          :horizontal_rule, :list, :definition_list, :block_html, :setext_header,
                          :block_math, :table, :footnote_definition, :link_definition, :abbrev_definition,
                          :block_extensions, :eob_marker, :paragraph]
        @span_parsers =  [:emphasis, :codespan, :autolink, :span_html, :footnote_marker, :link, :smart_quotes, :inline_math,
                         :span_extensions, :html_entity, :typographic_syms, :line_break, :escaped_chars]

      end
      private_class_method(:new, :allocate)


      # The source string provided on initialization is parsed into the @root element.
      def parse
        configure_parser
        parse_blocks(@root, adapt_source(source))
        update_tree(@root)
        correct_abbreviations_attributes
        replace_abbreviations(@root)
        @footnotes.each {|name,data| update_tree(data[:content])}
        @footnotes.each do |name, data|
          next if data.key?(:marker)
          line = data[:content].options[:location]
          warning("Footnote definition for '#{name}' on line #{line} is unreferenced - ignoring")
        end
      end

      #######
      protected
      #######

      # :doc:
      #
      # Update the parser specific link definitions with the data from +link_defs+ (the value of the
      # :link_defs option).
      #
      # The parameter +link_defs+ is a hash where the keys are possibly unnormalized link IDs and
      # the values are two element arrays consisting of the link target and a title (can be +nil+).
      def update_link_definitions(link_defs)
        link_defs.each {|k,v| @link_defs[normalize_link_id(k)] = v}
      end

      # Adapt the object to allow parsing like specified in the options.
      def configure_parser
        @parsers = {}
        (@block_parsers + @span_parsers).each do |name|
          if self.class.has_parser?(name)
            @parsers[name] = self.class.parser(name)
          else
            raise Kramdown::Error, "Unknown parser: #{name}"
          end
        end
        @span_start, @span_start_re = span_parser_regexps
      end

      # Create the needed span parser regexps.
      def span_parser_regexps(parsers = @span_parsers)
        span_start = /#{parsers.map {|name| @parsers[name].span_start}.join('|')}/
        [span_start, /(?=#{span_start})/]
      end

      # Parse all block-level elements in +text+ into the element +el+.
      def parse_blocks(el, text = nil)
        @stack.push([@tree, @src, @block_ial])
        @tree, @block_ial = el, nil
        @src = (text.nil? ? @src : ::Kramdown::Utils::StringScanner.new(text, el.options[:location]))

        status = catch(:stop_block_parsing) do
          while !@src.eos?
            @block_parsers.any? do |name|
              if @src.check(@parsers[name].start_re)
                send(@parsers[name].method)
              else
                false
              end
            end || begin
              warning('Warning: this should not occur - no block parser handled the line')
              add_text(@src.scan(/.*\n/))
            end
          end
        end

        @tree, @src, @block_ial = *@stack.pop
        status
      end

      # Update the tree by parsing all :+raw_text+ elements with the span-level parser (resets the
      # environment) and by updating the attributes from the IALs.
      def update_tree(element)
        last_blank = nil
        element.children.map! do |child|
          if child.type == :raw_text
            last_blank = nil
            reset_env(:src => ::Kramdown::Utils::StringScanner.new(child.value, element.options[:location]),
                      :text_type => :text)
            parse_spans(child)
            child.children
          elsif child.type == :eob
            update_attr_with_ial(child.attr, child.options[:ial]) if child.options[:ial]
            []
          elsif child.type == :blank
            if last_blank
              last_blank.value << child.value
              []
            else
              last_blank = child
              child
            end
          else
            last_blank = nil
            update_tree(child)
            update_attr_with_ial(child.attr, child.options[:ial]) if child.options[:ial]
            # DEPRECATED: option auto_id_stripping will be removed in 2.0 because then this will be
            # the default behaviour
            if child.type == :dt || (child.type == :header && @options[:auto_id_stripping])
              update_raw_text(child)
            end
            child
          end
        end.flatten!
      end

      # Parse all span-level elements in the source string of @src into +el+.
      #
      # If the parameter +stop_re+ (a regexp) is used, parsing is immediately stopped if the regexp
      # matches and if no block is given or if a block is given and it returns +true+.
      #
      # The parameter +parsers+ can be used to specify the (span-level) parsing methods that should
      # be used for parsing.
      #
      # The parameter +text_type+ specifies the type which should be used for created text nodes.
      def parse_spans(el, stop_re = nil, parsers = nil, text_type = @text_type)
        @stack.push([@tree, @text_type]) unless @tree.nil?
        @tree, @text_type = el, text_type

        span_start = @span_start
        span_start_re = @span_start_re
        span_start, span_start_re = span_parser_regexps(parsers) if parsers
        parsers = parsers || @span_parsers

        used_re = (stop_re.nil? ? span_start_re : /(?=#{Regexp.union(stop_re, span_start)})/)
        stop_re_found = false
        while !@src.eos? && !stop_re_found
          if result = @src.scan_until(used_re)
            add_text(result)
            if stop_re && @src.check(stop_re)
              stop_re_found = (block_given? ? yield : true)
            end
            processed = parsers.any? do |name|
              if @src.check(@parsers[name].start_re)
                send(@parsers[name].method)
                true
              else
                false
              end
            end unless stop_re_found
            add_text(@src.getch) if !processed && !stop_re_found
          else
            (add_text(@src.rest); @src.terminate) unless stop_re
            break
          end
        end

        @tree, @text_type = @stack.pop

        stop_re_found
      end

      # Reset the current parsing environment. The parameter +env+ can be used to set initial
      # values for one or more environment variables.
      def reset_env(opts = {})
        opts = {:text_type => :raw_text, :stack => []}.merge(opts)
        @src = opts[:src]
        @tree = opts[:tree]
        @block_ial = opts[:block_ial]
        @stack = opts[:stack]
        @text_type = opts[:text_type]
      end

      # Return the current parsing environment.
      def save_env
        [@src, @tree, @block_ial, @stack,  @text_type]
      end

      # Restore the current parsing environment.
      def restore_env(env)
        @src, @tree, @block_ial, @stack,  @text_type = *env
      end

      # Update the given attributes hash +attr+ with the information from the inline attribute list
      # +ial+ and all referenced ALDs.
      def update_attr_with_ial(attr, ial)
        ial[:refs].each do |ref|
          update_attr_with_ial(attr, ref) if ref = @alds[ref]
        end if ial[:refs]
        ial.each do |k,v|
          if k == IAL_CLASS_ATTR
            attr[k] = (attr[k] || '') << " #{v}"
            attr[k].lstrip!
          elsif k.kind_of?(String)
            attr[k] = v
          end
        end
      end

      # Update the raw text for automatic ID generation.
      def update_raw_text(item)
        raw_text = ''

        append_text = lambda do |child|
          if child.type == :text
            raw_text << child.value
          else
            child.children.each {|c| append_text.call(c)}
          end
        end

        append_text.call(item)
        item.options[:raw_text] = raw_text
      end

      # Create a new block-level element, taking care of applying a preceding block IAL if it
      # exists. This method should always be used for creating a block-level element!
      def new_block_el(*args)
        el = Element.new(*args)
        if @block_ial
          el.options[:ial] = @block_ial
          @block_ial = nil
        end
        el
      end

      @@parsers = {}

      # Struct class holding all the needed data for one block/span-level parser method.
      Data = Struct.new(:name, :start_re, :span_start, :method)

      # Add a parser method
      #
      # * with the given +name+,
      # * using +start_re+ as start regexp
      # * and, for span parsers, +span_start+ as a String that can be used in a regexp and
      #   which identifies the starting character(s)
      #
      # to the registry. The method name is automatically derived from the +name+ or can explicitly
      # be set by using the +meth_name+ parameter.
      def self.define_parser(name, start_re, span_start = nil, meth_name = "parse_#{name}")
        raise "A parser with the name #{name} already exists!" if @@parsers.has_key?(name)
        @@parsers[name] = Data.new(name, start_re, span_start, meth_name)
      end

      # Return the Data structure for the parser +name+.
      def self.parser(name = nil)
        @@parsers[name]
      end

      # Return +true+ if there is a parser called +name+.
      def self.has_parser?(name)
        @@parsers.has_key?(name)
      end

      # Regexp for matching indentation (one tab or four spaces)
      INDENT = /^(?:\t| {4})/
      # Regexp for matching the optional space (zero or up to three spaces)
      OPT_SPACE = / {0,3}/

      require 'kramdown/parser/kramdown/blank_line'
      require 'kramdown/parser/kramdown/eob'
      require 'kramdown/parser/kramdown/paragraph'
      require 'kramdown/parser/kramdown/header'
      require 'kramdown/parser/kramdown/blockquote'
      require 'kramdown/parser/kramdown/table'
      require 'kramdown/parser/kramdown/codeblock'
      require 'kramdown/parser/kramdown/horizontal_rule'
      require 'kramdown/parser/kramdown/list'
      require 'kramdown/parser/kramdown/link'
      require 'kramdown/parser/kramdown/extensions'
      require 'kramdown/parser/kramdown/footnote'
      require 'kramdown/parser/kramdown/html'
      require 'kramdown/parser/kramdown/escaped_chars'
      require 'kramdown/parser/kramdown/html_entity'
      require 'kramdown/parser/kramdown/line_break'
      require 'kramdown/parser/kramdown/typographic_symbol'
      require 'kramdown/parser/kramdown/autolink'
      require 'kramdown/parser/kramdown/codespan'
      require 'kramdown/parser/kramdown/emphasis'
      require 'kramdown/parser/kramdown/smart_quotes'
      require 'kramdown/parser/kramdown/math'
      require 'kramdown/parser/kramdown/abbreviation'

    end

  end

end
