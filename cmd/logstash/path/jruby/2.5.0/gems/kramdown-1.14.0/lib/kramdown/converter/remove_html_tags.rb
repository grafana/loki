# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'kramdown/converter'

module Kramdown

  module Converter

    # Removes all block (and optionally span) level HTML tags from the element tree.
    #
    # This converter can be used on parsed HTML documents to get an element tree that will only
    # contain native kramdown elements.
    #
    # *Note* that the returned element tree may not be fully conformant (i.e. the content models of
    # *some elements may be violated)!
    #
    # This converter modifies the given tree in-place and returns it.
    class RemoveHtmlTags < Base

      def initialize(root, options)
        super
        @options[:template] = ''
      end

      def convert(el)
        children = el.children.dup
        index = 0
        while index < children.length
          if [:xml_pi].include?(children[index].type) ||
              (children[index].type == :html_element && %w[style script].include?(children[index].value))
            children[index..index] = []
          elsif children[index].type == :html_element &&
            ((@options[:remove_block_html_tags] && children[index].options[:category] == :block) ||
             (@options[:remove_span_html_tags] && children[index].options[:category] == :span))
            children[index..index] = children[index].children
          else
            convert(children[index])
            index += 1
          end
        end
        el.children = children
        el
      end

    end

  end
end
