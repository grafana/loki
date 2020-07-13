require 'stringio'

module Clamp

  module Help

    def usage(usage)
      @declared_usage_descriptions ||= []
      @declared_usage_descriptions << usage
    end

    attr_reader :declared_usage_descriptions

    def description=(description)
      @description = description.dup
      if @description =~ /^\A\n*( +)/
        indent = $1
        @description.gsub!(/^#{indent}/, '')
      end
      @description.strip!
    end

    def banner(description)
      self.description = description
    end

    attr_reader :description

    def derived_usage_description
      parts = ["[OPTIONS]"]
      parts += parameters.map { |a| a.name }
      parts.join(" ")
    end

    def usage_descriptions
      declared_usage_descriptions || [derived_usage_description]
    end

    def help(invocation_path, builder = Builder.new)
      help = builder
      help.add_usage(invocation_path, usage_descriptions)
      help.add_description(description)
      if has_parameters?
        help.add_list("Parameters", parameters)
      end
      if has_subcommands?
        help.add_list("Subcommands", recognised_subcommands)
      end
      help.add_list("Options", recognised_options)
      help.string
    end

    class Builder

      def initialize
        @out = StringIO.new
      end

      def string
        @out.string
      end

      def add_usage(invocation_path, usage_descriptions)
        puts "Usage:"
        usage_descriptions.each do |usage|
          puts "    #{invocation_path} #{usage}".rstrip
        end
      end

      def add_description(description)
        if description
          puts ""
          puts description.gsub(/^/, "  ")
        end
      end

      DETAIL_FORMAT = "    %-29s %s"

      def add_list(heading, items)
        puts "\n#{heading}:"
        items.each do |item|
          label, description = item.help
          description.each_line do |line|
            puts DETAIL_FORMAT % [label, line]
            label = ''
          end
        end
      end

      private

      def puts(*args)
        @out.puts(*args)
      end

    end

  end

end
