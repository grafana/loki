module Clamp

  TRUTHY_VALUES = %w(1 yes enable on true)

  def self.truthy?(arg)
    TRUTHY_VALUES.include?(arg.to_s.downcase)
  end

end
