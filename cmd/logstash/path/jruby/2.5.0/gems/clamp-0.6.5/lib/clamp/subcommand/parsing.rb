require 'clamp/subcommand/execution'

module Clamp
  module Subcommand

    module Parsing

      protected

      def parse_subcommand
        return false unless self.class.has_subcommands?
        self.extend(Subcommand::Execution)
      end

      private

      def default_subcommand_name
        self.class.default_subcommand || request_help
      end

    end

  end
end
