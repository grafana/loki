require "gem_publisher/cli_facade"

module GemPublisher
  class Builder
    def initialize(cli_facade = CliFacade.new)
      @cli_facade = cli_facade
    end

    def build(gemspec)
      @cli_facade.execute("gem", "build", gemspec)[/File:\s+(.+)/, 1]
    end
  end
end
