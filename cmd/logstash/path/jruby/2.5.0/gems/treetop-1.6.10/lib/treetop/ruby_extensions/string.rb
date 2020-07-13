class String
  def column_of(index)
    return 1 if index == 0
    newline_index = rindex("\n", index - 1)
    if newline_index
      index - newline_index
    else
      index + 1
    end
  end
  
  def line_of(index)
    self[0...index].count("\n") + 1
  end

  unless method_defined?(:blank?)
    def blank?
      self == ""
    end
  end

  # The following methods are lifted from Facets 2.0.2
  def tabto(n)
    if self =~ /^( *)\S/
      # Inlined due to collision with ActiveSupport 4.0: indent(n - $1.length)
      m = n - $1.length
      if m >= 0
        gsub(/^/, ' ' * m)
      else
        gsub(/^ {0,#{-m}}/, "")
      end
    else
      self
    end
  end

  def treetop_camelize
    to_s.gsub(/\/(.?)/){ "::" + $1.upcase }.gsub(/(^|_)(.)/){ $2.upcase }
  end
end
