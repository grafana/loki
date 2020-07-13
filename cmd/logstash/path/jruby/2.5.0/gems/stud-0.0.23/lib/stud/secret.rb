
# A class for holding a secret. The main goal is to prevent the common mistake
# of accidentally logging or printing passwords or other secrets.
#
# See
# <https://github.com/jordansissel/software-patterns/blob/master/dont-log-secrets/ruby/>
# for a discussion of why this implementation is useful.
module Stud
  class Secret
    # Initialize a new secret with a given value.
    #
    # value - anything you want to keep secret from loggers, etc.
    def initialize(secret_value)
      # Redefine the 'value' method on this instance. This exposes no instance
      # variables to be accidentally leaked by things like awesome_print, etc.
      # This makes any #value call return the secret value.
      (class << self; self; end).class_eval do
        define_method(:value) { secret_value }
      end
    end # def initialize

    # Emit simply "<secret>" when printed or logged.
    def to_s
      return "<secret>"
    end # def to_s

    alias_method :inspect, :to_s

    # Get the secret value.
    def value
      # Nothing, this will be filled in by Secret.new
      # But we'll still document this so rdoc/yard know the method exists.
    end # def value
  end # class Secret
end # class Stud
