# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#
# All the code in this file is backported from Ruby 1.8.7 sothat kramdown works under 1.8.5
#
# :stopdoc:

require 'rbconfig'

if RUBY_VERSION <= '1.8.6'
  require 'rexml/parsers/baseparser'
  module REXML
    module Parsers
      class BaseParser
        UNAME_STR= "(?:#{NCNAME_STR}:)?#{NCNAME_STR}" unless const_defined?(:UNAME_STR)
      end
    end
  end

  if !String.instance_methods.include?("start_with?")

    class String
      def start_with?(str)
        self[0, str.length] == str
      end
      def end_with?(str)
        self[-str.length, str.length] == str
      end
    end

  end

end

if !Symbol.instance_methods.include?("<=>")

  class Symbol
    def <=>(other)
      self.to_s <=> other.to_s
    end
  end

end
