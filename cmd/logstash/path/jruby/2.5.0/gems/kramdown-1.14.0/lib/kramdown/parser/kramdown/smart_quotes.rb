# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#
#--
# Parts of this file are based on code from RubyPants:
#
# = RubyPants -- SmartyPants ported to Ruby
#
# Ported by Christian Neukirchen <mailto:chneukirchen@gmail.com>
#   Copyright (C) 2004 Christian Neukirchen
#
# Incooporates ideas, comments and documentation by Chad Miller
#   Copyright (C) 2004 Chad Miller
#
# Original SmartyPants by John Gruber
#   Copyright (C) 2003 John Gruber
#
#
# = RubyPants -- SmartyPants ported to Ruby
#
#
# [snip]
#
# == Authors
#
# John Gruber did all of the hard work of writing this software in
# Perl for Movable Type and almost all of this useful documentation.
# Chad Miller ported it to Python to use with Pyblosxom.
#
# Christian Neukirchen provided the Ruby port, as a general-purpose
# library that follows the *Cloth API.
#
#
# == Copyright and License
#
# === SmartyPants license:
#
# Copyright (c) 2003 John Gruber
# (http://daringfireball.net)
# All rights reserved.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
#
# * Redistributions of source code must retain the above copyright
#   notice, this list of conditions and the following disclaimer.
#
# * Redistributions in binary form must reproduce the above copyright
#   notice, this list of conditions and the following disclaimer in
#   the documentation and/or other materials provided with the
#   distribution.
#
# * Neither the name "SmartyPants" nor the names of its contributors
#   may be used to endorse or promote products derived from this
#   software without specific prior written permission.
#
# This software is provided by the copyright holders and contributors
# "as is" and any express or implied warranties, including, but not
# limited to, the implied warranties of merchantability and fitness
# for a particular purpose are disclaimed. In no event shall the
# copyright owner or contributors be liable for any direct, indirect,
# incidental, special, exemplary, or consequential damages (including,
# but not limited to, procurement of substitute goods or services;
# loss of use, data, or profits; or business interruption) however
# caused and on any theory of liability, whether in contract, strict
# liability, or tort (including negligence or otherwise) arising in
# any way out of the use of this software, even if advised of the
# possibility of such damage.
#
# === RubyPants license
#
# RubyPants is a derivative work of SmartyPants and smartypants.py.
#
# Redistribution and use in source and binary forms, with or without
# modification, are permitted provided that the following conditions
# are met:
#
# * Redistributions of source code must retain the above copyright
#   notice, this list of conditions and the following disclaimer.
#
# * Redistributions in binary form must reproduce the above copyright
#   notice, this list of conditions and the following disclaimer in
#   the documentation and/or other materials provided with the
#   distribution.
#
# This software is provided by the copyright holders and contributors
# "as is" and any express or implied warranties, including, but not
# limited to, the implied warranties of merchantability and fitness
# for a particular purpose are disclaimed. In no event shall the
# copyright owner or contributors be liable for any direct, indirect,
# incidental, special, exemplary, or consequential damages (including,
# but not limited to, procurement of substitute goods or services;
# loss of use, data, or profits; or business interruption) however
# caused and on any theory of liability, whether in contract, strict
# liability, or tort (including negligence or otherwise) arising in
# any way out of the use of this software, even if advised of the
# possibility of such damage.
#
# == Links
#
# John Gruber:: http://daringfireball.net
# SmartyPants:: http://daringfireball.net/projects/smartypants
#
# Chad Miller:: http://web.chad.org
#
# Christian Neukirchen:: http://kronavita.de/chris
#
#++
#

module Kramdown
  module Parser
    class Kramdown

      SQ_PUNCT = '[!"#\$\%\'()*+,\-.\/:;<=>?\@\[\\\\\]\^_`{|}~]'
      SQ_CLOSE = %![^\ \\\\\t\r\n\\[{(-]!

      SQ_RULES = [
                  [/("|')(?=[_*]{1,2}\S)/, [:lquote1]],
                  [/("|')(?=#{SQ_PUNCT}(?!\.\.)\B)/, [:rquote1]],
                  # Special case for double sets of quotes, e.g.:
                  #   <p>He said, "'Quoted' words in a larger quote."</p>
                  [/(\s?)"'(?=\w)/, [1, :ldquo, :lsquo]],
                  [/(\s?)'"(?=\w)/, [1, :lsquo, :ldquo]],
                  # Special case for decade abbreviations (the '80s):
                  [/(\s?)'(?=\d\ds)/, [1, :rsquo]],

                  # Get most opening single/double quotes:
                  [/(\s)('|")(?=\w)/, [1, :lquote2]],
                  # Single/double closing quotes:
                  [/(#{SQ_CLOSE})('|")/, [1, :rquote2]],
                  # Special case for e.g. "<i>Custer</i>'s Last Stand."
                  [/("|')(?=\s|s\b|$)/, [:rquote1]],
                  # Any remaining single quotes should be opening ones:
                  [/(.?)'/m, [1, :lsquo]],
                  [/(.?)"/m, [1, :ldquo]],
                 ] #'"

      SQ_SUBSTS = {
        [:rquote1, '"'] => :rdquo,
        [:rquote1, "'"] => :rsquo,
        [:rquote2, '"'] => :rdquo,
        [:rquote2, "'"] => :rsquo,
        [:lquote1, '"'] => :ldquo,
        [:lquote1, "'"] => :lsquo,
        [:lquote2, '"'] => :ldquo,
        [:lquote2, "'"] => :lsquo,
      }
      SMART_QUOTES_RE = /[^\\]?["']/

      # Parse the smart quotes at current location.
      def parse_smart_quotes
        start_line_number = @src.current_line_number
        substs = SQ_RULES.find {|reg, subst| @src.scan(reg)}[1]
        substs.each do |subst|
          if subst.kind_of?(Integer)
            add_text(@src[subst])
          else
            val = SQ_SUBSTS[[subst, @src[subst.to_s[-1,1].to_i]]] || subst
            @tree.children << Element.new(:smart_quote, val, nil, :location => start_line_number)
          end
        end
      end
      define_parser(:smart_quotes, SMART_QUOTES_RE, '[^\\\\]?["\']')

    end
  end
end
