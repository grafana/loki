# frozen_string_literal: true

module Faraday
  # DependencyLoader helps Faraday adapters and middleware load dependencies.
  module DependencyLoader
    attr_reader :load_error

    # Executes a block which should try to require and reference dependent
    # libraries
    def dependency(lib = nil)
      lib ? require(lib) : yield
    rescue LoadError, NameError => e
      self.load_error = e
    end

    def new(*)
      unless loaded?
        raise "missing dependency for #{self}: #{load_error.message}"
      end

      super
    end

    def loaded?
      load_error.nil?
    end

    def inherited(subclass)
      super
      subclass.send(:load_error=, load_error)
    end

    private

    attr_writer :load_error
  end
end
