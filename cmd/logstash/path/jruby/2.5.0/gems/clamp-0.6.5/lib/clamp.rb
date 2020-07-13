require 'clamp/version'

require 'clamp/command'

def Clamp(&block)
  Class.new(Clamp::Command, &block).run
end
