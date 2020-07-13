if RUBY_ENGINE == 'ruby' || RUBY_ENGINE == 'rbx'
  Object.send(:remove_const, :FFI) if defined?(::FFI)
  begin
    require RUBY_VERSION.split('.')[0, 2].join('.') + '/ffi_c'
  rescue Exception
    require 'ffi_c'
  end

  require 'ffi/ffi'

elsif RUBY_ENGINE == 'jruby' && Gem::Version.new(RUBY_ENGINE_VERSION) >= Gem::Version.new("9.3.pre")
  JRuby::Util.load_ext("org.jruby.ext.ffi.FFIService")
  require 'ffi/ffi'

elsif RUBY_ENGINE == 'truffleruby' && Gem::Version.new(RUBY_ENGINE_VERSION) >= Gem::Version.new("20.1.0-dev-a")
  require 'truffleruby/ffi_backend'
  require 'ffi/ffi'

else
  # Remove the ffi gem dir from the load path, then reload the internal ffi implementation
  $LOAD_PATH.delete(File.dirname(__FILE__))
  $LOAD_PATH.delete(File.join(File.dirname(__FILE__), 'ffi'))
  unless $LOADED_FEATURES.nil?
    $LOADED_FEATURES.delete(__FILE__)
    $LOADED_FEATURES.delete('ffi.rb')
  end
  require 'ffi.rb'
end
