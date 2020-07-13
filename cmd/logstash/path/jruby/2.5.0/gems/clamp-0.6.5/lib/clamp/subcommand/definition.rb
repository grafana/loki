module Clamp
  module Subcommand

    class Definition < Struct.new(:name, :description, :subcommand_class)

      def initialize(names, description, subcommand_class)
        @names = Array(names)
        @description = description
        @subcommand_class = subcommand_class
      end

      attr_reader :names, :description, :subcommand_class

      def is_called?(name)
        names.member?(name)
      end

      def help
        [names.join(", "), description]
      end

    end

  end
end
