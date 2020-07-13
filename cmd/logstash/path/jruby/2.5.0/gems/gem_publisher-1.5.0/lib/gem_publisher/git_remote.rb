require "gem_publisher/cli_facade"

module GemPublisher
  class GitRemote
    def initialize(name = "origin", cli_facade = CliFacade.new)
      @remote_name = name
      @cli_facade  = cli_facade
    end

    def tags
      s = git("ls-remote", "--tags", @remote_name)
      s.scan(%r{refs/tags/(.+)}).map(&:first).reject { |t| t =~ /\^\{\}$/ }
    end

    def add_tag(tag_name, commit_ish = "HEAD")
      sha1 = git("rev-parse", commit_ish).chomp
      git "update-ref", "refs/tags/#{tag_name}", sha1
      git "push", @remote_name, "tag", tag_name
    end

  private
    def git(*args)
      @cli_facade.execute(*["git"] + args)
    end
  end
end
