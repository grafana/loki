# coding: utf-8

require 'archive/tar/minitar'

if defined?(::Minitar) && ::Minitar != Archive::Tar::Minitar
  warn <<-EOS
::Minitar is already defined.
This will conflict with future versions of minitar.
  EOS
else
  ::Minitar = Archive::Tar::Minitar
end
