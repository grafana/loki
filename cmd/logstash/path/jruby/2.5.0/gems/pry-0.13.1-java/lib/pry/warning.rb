# frozen_string_literal: true

class Pry
  # @api private
  # @since v0.13.0
  module Warning
    # Prints a warning message with exact file and line location, similar to how
    # Ruby's -W prints warnings.
    #
    # @param [String] message
    # @return [void]
    def self.warn(message)
      if Kernel.respond_to?(:caller_locations)
        location = caller_locations(2..2).first
        path = location.path
        lineno = location.lineno
      else
        # Ruby 1.9.3 support.
        frame = caller[1].split(':') # rubocop:disable Performance/Caller
        path = frame.first
        lineno = frame[1]
      end

      Kernel.warn("#{path}:#{lineno}: warning: #{message}")
    end
  end
end
