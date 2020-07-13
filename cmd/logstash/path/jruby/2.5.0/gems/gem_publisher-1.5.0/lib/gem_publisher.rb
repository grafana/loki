require "gem_publisher/publisher"
require "gem_publisher/version"

module GemPublisher

  # Publish a gem based on the supplied gemspec via supplied method, iff this
  # version has not already been released and tagged in the origin Git
  # repository.
  #
  # Version is derived from the gemspec.
  #
  # If a remote tag matching the version already exists, nothing is done.
  # Otherwise, the gem is built, pushed, and tagged.
  #
  # Tags are of the form "v1.2.3" by default, and generated tags follow
  # this pattern; you can override this by passing in the :tag_prefix option.
  #
  # Method should be one of :rubygems or :gemfury, and the requisite
  # credentials for the corresponding push command line tools must exist.
  #
  # Returns the gem file name if a gem was published; nil otherwise. A
  # CliFacade::Error will be raised if a command fails.
  #
  def self.publish_if_updated(gemspec, method=:rubygems, options={})
    publisher = Publisher.new(gemspec, :tag_prefix => options.delete(:tag_prefix))
    publisher.publish_if_updated(method, options)
  end
end
