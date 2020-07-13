require 'date'
require 'gems/configuration'
require 'gems/request'
require 'json'

module Gems
  class Client
    include Gems::Request
    attr_accessor(*Configuration::VALID_OPTIONS_KEYS)

    def initialize(options = {})
      options = Gems.options.merge(options)
      Configuration::VALID_OPTIONS_KEYS.each do |key|
        send("#{key}=", options[key])
      end
    end

    # Returns some basic information about the given gem
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem.
    # @return [Hash]
    # @example
    #   Gems.info 'rails'
    def info(gem_name)
      response = get("/api/v1/gems/#{gem_name}.json")
      JSON.parse(response)
    rescue JSON::ParserError
      {}
    end

    # Returns an array of active gems that match the query
    #
    # @authenticated false
    # @param query [String] A term to search for.
    # @param options [Hash] A customizable set of options.
    # @option options [Integer] :page
    # @return [Array<Hash>]
    # @example
    #   Gems.search 'cucumber'
    def search(query, options = {})
      response = get('/api/v1/search.json', options.merge(:query => query))
      JSON.parse(response)
    end

    # List all gems that you own
    #
    # @authenticated true
    # @param user_handle [String] The handle of a user.
    # @return [Array]
    # @example
    #   Gems.gems
    def gems(user_handle = nil)
      response = user_handle ? get("/api/v1/owners/#{user_handle}/gems.json") : get('/api/v1/gems.json')
      JSON.parse(response)
    end

    # Submit a gem to RubyGems.org or another host
    #
    # @authenticated true
    # @param gem [File] A built gem.
    # @param host [String] A RubyGems compatible host to use.
    # @return [String]
    # @example
    #   Gems.push File.new 'pkg/gemcutter-0.2.1.gem'
    def push(gem, host = Configuration::DEFAULT_HOST)
      post('/api/v1/gems', gem.read, 'application/octet-stream', host)
    end

    # Remove a gem from RubyGems.org's index
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem.
    # @param gem_version [String] The version of a gem.
    # @param options [Hash] A customizable set of options.
    # @option options [String] :platform
    # @return [String]
    # @example
    #   Gems.yank "gemcutter", "0.2.1", {:platform => "x86-darwin-10"}
    def yank(gem_name, gem_version = nil, options = {})
      gem_version ||= info(gem_name)['version']
      delete('/api/v1/gems/yank', options.merge(:gem_name => gem_name, :version => gem_version))
    end

    # Update a previously yanked gem back into RubyGems.org's index
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem.
    # @param gem_version [String] The version of a gem.
    # @param options [Hash] A customizable set of options.
    # @option options [String] :platform
    # @return [String]
    # @example
    #   Gems.unyank "gemcutter", "0.2.1", {:platform => "x86-darwin-10"}
    def unyank(gem_name, gem_version = nil, options = {})
      gem_version ||= info(gem_name)['version']
      put('/api/v1/gems/unyank', options.merge(:gem_name => gem_name, :version => gem_version))
    end

    # Returns an array of gem version details
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem.
    # @return [Hash]
    # @example
    #   Gems.versions 'coulda'
    def versions(gem_name)
      response = get("/api/v1/versions/#{gem_name}.json")
      JSON.parse(response)
    end

    # Returns an hash of gem latest version
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem.
    # @return [Hash]
    # @example
    #   Gems.latest_version 'coulda'
    def latest_version(gem_name)
      response = get("/api/v1/versions/#{gem_name}/latest.json")
      JSON.parse(response)
    end

    # Returns the total number of downloads for a particular gem
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem.
    # @param gem_version [String] The version of a gem.
    # @return [Hash]
    # @example
    #   Gems.total_downloads 'rails_admin', '0.0.1'
    def total_downloads(gem_name = nil, gem_version = nil)
      response = gem_name ? get("/api/v1/downloads/#{gem_name}-#{gem_version || info(gem_name)['version']}.json") : get('/api/v1/downloads.json')
      JSON.parse(response, :symbolize_names => true)
    end

    # Returns an array containing the top 50 downloaded gem versions of all time
    #
    # @authenticated false
    # @return [Array]
    # @example
    #   Gems.most_downloaded
    def most_downloaded
      response = get('/api/v1/downloads/all.json')
      JSON.parse(response)['gems']
    end

    # Returns the number of downloads by day for a particular gem version
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem.
    # @param gem_version [String] The version of a gem.
    # @param from [Date] Search start date.
    # @param to [Date] Search end date.
    # @return [Hash]
    # @example
    #   Gems.downloads 'coulda', '0.6.3', Date.today - 30, Date.today
    def downloads(gem_name, gem_version = nil, from = nil, to = Date.today)
      gem_version ||= info(gem_name)['version']
      response = from ? get("/api/v1/versions/#{gem_name}-#{gem_version}/downloads/search.json", :from => from.to_s, :to => to.to_s) : get("/api/v1/versions/#{gem_name}-#{gem_version}/downloads.json")
      JSON.parse(response)
    end

    # View all owners of a gem that you own
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem.
    # @return [Array]
    # @example
    #   Gems.owners 'gemcutter'
    def owners(gem_name)
      response = get("/api/v1/gems/#{gem_name}/owners.json")
      JSON.parse(response)
    end

    # Add an owner to a RubyGem you own, giving that user permission to manage it
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem.
    # @param owner [String] The email address of the user you want to add.
    # @return [String]
    # @example
    #   Gems.add_owner 'gemcutter', 'josh@technicalpickles.com'
    def add_owner(gem_name, owner)
      post("/api/v1/gems/#{gem_name}/owners", :email => owner)
    end

    # Remove a user's permission to manage a RubyGem you own
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem.
    # @param owner [String] The email address of the user you want to remove.
    # @return [String]
    # @example
    #   Gems.remove_owner 'gemcutter', 'josh@technicalpickles.com'
    def remove_owner(gem_name, owner)
      delete("/api/v1/gems/#{gem_name}/owners", :email => owner)
    end

    # List the webhooks registered under your account
    #
    # @authenticated true
    # @return [Hash]
    # @example
    #   Gems.web_hooks
    def web_hooks
      response = get('/api/v1/web_hooks.json')
      JSON.parse(response)
    end

    # Create a webhook
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem. Specify "*" to add the hook to all gems.
    # @param url [String] The URL of the web hook.
    # @return [String]
    # @example
    #   Gems.add_web_hook 'rails', 'http://example.com'
    def add_web_hook(gem_name, url)
      post('/api/v1/web_hooks', :gem_name => gem_name, :url => url)
    end

    # Remove a webhook
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem. Specify "*" to remove the hook from all gems.
    # @param url [String] The URL of the web hook.
    # @return [String]
    # @example
    #   Gems.remove_web_hook 'rails', 'http://example.com'
    def remove_web_hook(gem_name, url)
      delete('/api/v1/web_hooks/remove', :gem_name => gem_name, :url => url)
    end

    # Test fire a webhook
    #
    # @authenticated true
    # @param gem_name [String] The name of a gem. Specify "*" to fire the hook for all gems.
    # @param url [String] The URL of the web hook.
    # @return [String]
    # @example
    #   Gems.fire_web_hook 'rails', 'http://example.com'
    def fire_web_hook(gem_name, url)
      post('/api/v1/web_hooks/fire', :gem_name => gem_name, :url => url)
    end

    # Returns the 50 gems most recently added to RubyGems.org (for the first time)
    #
    # @authenticated false
    # @param options [Hash] A customizable set of options.
    # @return [Array]
    # @example
    #   Gem.latest
    def latest(options = {})
      response = get('/api/v1/activity/latest.json', options)
      JSON.parse(response)
    end

    # Returns the 50 most recently updated gems
    #
    # @authenticated false
    # @param options [Hash] A customizable set of options.
    # @return [Array]
    # @example
    #   Gem.just_updated
    def just_updated(options = {})
      response = get('/api/v1/activity/just_updated.json', options)
      JSON.parse(response)
    end

    # Retrieve your API key using HTTP basic auth
    #
    # @authenticated true
    # @return [String]
    # @example
    #   Gems.configure do |config|
    #     config.username = 'nick@gemcutter.org'
    #     config.password = 'schwwwwing'
    #   end
    #   Gems.api_key
    def api_key
      get('/api/v1/api_key')
    end

    # Returns an array of hashes for all versions of given gems
    #
    # @authenticated false
    # @param gems [Array] A list of gem names
    # @return [Array]
    # @example
    #   Gems.dependencies 'rails', 'thor'
    def dependencies(*gems)
      response = get('/api/v1/dependencies', :gems => gems.join(','))
      Marshal.load(response)
    end

    # Returns an array of all the reverse dependencies to the given gem.
    #
    # @authenticated false
    # @param gem_name [String] The name of a gem
    # @param options [Hash] A customizable set of options.
    # @return [Array]
    # @example
    #   Gems.reverse_dependencies 'money'
    def reverse_dependencies(gem_name, options = {})
      response = get("/api/v1/gems/#{gem_name}/reverse_dependencies.json", options)
      JSON.parse(response)
    end
  end
end
