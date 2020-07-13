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

      CODESPAN_DELIMITER = /`+/

      # Parse the codespan at the current scanner location.
      def parse_codespan
        start_line_number = @src.current_line_number
        result = @src.scan(CODESPAN_DELIMITER)
        simple = (result.length == 1)
        saved_pos = @src.save_pos

        if simple && @src.pre_match =~ /\s\Z/ && @src.match?(/\s/)
          add_text(result)
          return
        end

        if text = @src.scan_until(/#{result}/)
          text.sub!(/#{result}\Z/, '')
          if !simple
            text = text[1..-1] if text[0..0] == ' '
            text = text[0..-2] if text[-1..-1] == ' '
          end
          @tree.children << Element.new(:codespan, text, nil, :location => start_line_number)
        else
          @src.revert_pos(saved_pos)
          add_text(result)
        end
      end
      define_parser(:codespan, CODESPAN_DELIMITER, '`')

    end
  end
end
