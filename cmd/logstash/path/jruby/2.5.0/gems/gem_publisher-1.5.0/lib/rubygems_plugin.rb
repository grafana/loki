require "rubygems/command_manager"
require "rubygems/command"
require "gem_publisher"

class Gem::Commands::PublishCommand < Gem::Command
  def initialize
    super "publish", "Automatically publish a gem if the version has been updated"

    defaults.merge! :gem_repository => "rubygems"

    add_option(
      "-rREPOSITORY", "--repository=REPOSITORY",
      "Set the gem repository (rubygems or gemfury)",
      "Default: #{defaults[:gem_repository]}") do |value, options|
      options[:gem_repository] = value
    end

    add_option(
      "-aAS", "--as=AS",
      "Specify a shared account to publish the gem (gemfury only)"
    ) do |value, options|
      options[:as] = value
    end
  end

  def usage # :nodoc:
    "#{program_name} GEMFILE [GEMFILE ...]"
  end

  def execute
    options[:args].each do |gemspec|
      Dir.chdir(File.dirname(gemspec)) do
        new_opts = {}
        new_opts[:as] = options[:as] if options[:as]

        gem = GemPublisher.publish_if_updated(File.basename(gemspec), options[:gem_repository], new_opts)

        if gem
          $stderr.puts "Published #{gem}"
        else
          $stderr.puts "Nothing to do for #{gemspec}"
        end
      end
    end
  end
end

Gem::CommandManager.instance.register_command :publish
