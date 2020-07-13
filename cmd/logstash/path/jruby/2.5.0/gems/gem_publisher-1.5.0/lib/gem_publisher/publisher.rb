require "gem_publisher/git_remote"
require "gem_publisher/builder"
require "gem_publisher/pusher"
require "rubygems/specification"

module GemPublisher
  class Publisher
    attr_accessor :git_remote, :builder, :pusher
    attr_reader :version

    # Supported options:
    #   :tag_prefix - use a custom prefix for Git tags (defaults to 'v')
    def initialize(gemspec, options = {})
      @gemspec = gemspec
      @tag_prefix = options[:tag_prefix] || 'v'

      @version = eval(File.read(gemspec), TOPLEVEL_BINDING).version.to_s

      @git_remote = GitRemote.new
      @builder    = Builder.new
      @pusher     = Pusher.new
    end

    # Publish the gem if its version has changed since the last release.
    #
    # Supported options:
    #   :as - specify a shared account to publish the gem (Gemfury only)
    def publish_if_updated(method, options = {})
      return if version_released?
      @builder.build(@gemspec).tap { |gem|
        tag_prefix = options[:tag_prefix] || 'v'
        @pusher.push gem, method, options
        @git_remote.add_tag "#{@tag_prefix}#{@version}"
      }
    end

    def version_released?
      releases = @git_remote.tags.
        select { |t| t =~ /^#{@tag_prefix}\d+(\.\d+)+/ }.
        map { |t| t.scan(/\d+/).map(&:to_i) }
      this_release = @version.split(/\./).map(&:to_i)
      releases.include?(this_release)
    end
  end
end
