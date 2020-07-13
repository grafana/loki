# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown
  module Parser
    class Kramdown

      EOB_MARKER = /^\^\s*?\n/

      # Parse the EOB marker at the current location.
      def parse_eob_marker
        @src.pos += @src.matched_size
        @tree.children << new_block_el(:eob)
        true
      end
      define_parser(:eob_marker, EOB_MARKER)

    end
  end
end
