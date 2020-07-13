# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'rexml/parsers/baseparser'

module Kramdown

  module Utils

    # Provides convenience methods for HTML related tasks.
    #
    # *Note* that this module has to be mixed into a class that has a @root (containing an element
    # of type :root) and an @options (containing an options hash) instance variable so that some of
    # the methods can work correctly.
    module Html

      # Convert the entity +e+ to a string. The optional parameter +original+ may contain the
      # original representation of the entity.
      #
      # This method uses the option +entity_output+ to determine the output form for the entity.
      def entity_to_str(e, original = nil)
        entity_output = @options[:entity_output]

        if e.char.respond_to?(:encoding) && entity_output == :as_char &&
            (c = e.char.encode(@root.options[:encoding]) rescue nil) &&
            ((c = e.char) == '"' || !ESCAPE_MAP.has_key?(c))
          c
        elsif (entity_output == :as_input || entity_output == :as_char) && original
          original
        elsif (entity_output == :symbolic || ESCAPE_MAP.has_key?(e.char)) && !e.name.nil?
          "&#{e.name};"
        else # default to :numeric
          "&##{e.code_point};"
        end
      end

      # Return the HTML representation of the attributes +attr+.
      def html_attributes(attr)
        attr.map {|k,v| v.nil? || (k == 'id' && v.strip.empty?) ? '' : " #{k}=\"#{escape_html(v.to_s, :attribute)}\"" }.join('')
      end

      # :stopdoc:
      ESCAPE_MAP = {
        '<' => '&lt;',
        '>' => '&gt;',
        '&' => '&amp;',
        '"' => '&quot;'
      }
      ESCAPE_ALL_RE = /<|>|&/
      ESCAPE_TEXT_RE = Regexp.union(REXML::Parsers::BaseParser::REFERENCE_RE, /<|>|&/)
      ESCAPE_ATTRIBUTE_RE = Regexp.union(REXML::Parsers::BaseParser::REFERENCE_RE, /<|>|&|"/)
      ESCAPE_RE_FROM_TYPE = {
        :all => ESCAPE_ALL_RE,
        :text => ESCAPE_TEXT_RE,
        :attribute => ESCAPE_ATTRIBUTE_RE
      }
      # :startdoc:

      # Escape the special HTML characters in the string +str+. The parameter +type+ specifies what
      # is escaped: :all - all special HTML characters except the quotation mark as well as
      # entities, :text - all special HTML characters except the quotation mark but no entities and
      # :attribute - all special HTML characters including the quotation mark but no entities.
      def escape_html(str, type = :all)
        str.gsub(ESCAPE_RE_FROM_TYPE[type]) {|m| ESCAPE_MAP[m] || m}
      end

    end

  end

end
