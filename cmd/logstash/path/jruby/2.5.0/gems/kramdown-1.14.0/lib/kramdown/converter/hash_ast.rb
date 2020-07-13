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

    # Converts a Kramdown::Document to a nested hash for further processing or debug output.
    class HashAST < Base

      def convert(el)
        hash = {:type => el.type}
        hash[:attr] = el.attr unless el.attr.empty?
        hash[:value] = el.value unless el.value.nil?
        hash[:options] = el.options unless el.options.empty?
        unless el.children.empty?
          hash[:children] = []
          el.children.each {|child| hash[:children] << convert(child)}
        end
        hash
      end

    end

    HashAst = HashAST

  end
end
