module Gems
  class Version
    MAJOR = 1 unless defined? Gems::Version::MAJOR
    MINOR = 2 unless defined? Gems::Version::MINOR
    PATCH = 0 unless defined? Gems::Version::PATCH
    PRE = nil unless defined? Gems::Version::PRE

    class << self
      # @return [String]
      def to_s
        [MAJOR, MINOR, PATCH, PRE].compact.join('.')
      end
    end
  end

  VERSION = Version.to_s
end
