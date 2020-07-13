require 'concurrent/utility/engine'

require 'concurrent/synchronization/abstract_object'
require 'concurrent/utility/native_extension_loader' # load native parts first
Concurrent.load_native_extensions

require 'concurrent/synchronization/mri_object'
require 'concurrent/synchronization/jruby_object'
require 'concurrent/synchronization/rbx_object'
require 'concurrent/synchronization/truffleruby_object'
require 'concurrent/synchronization/object'
require 'concurrent/synchronization/volatile'

require 'concurrent/synchronization/abstract_lockable_object'
require 'concurrent/synchronization/mutex_lockable_object'
require 'concurrent/synchronization/jruby_lockable_object'
require 'concurrent/synchronization/rbx_lockable_object'

require 'concurrent/synchronization/lockable_object'

require 'concurrent/synchronization/condition'
require 'concurrent/synchronization/lock'

module Concurrent
  # {include:file:docs-source/synchronization.md}
  # {include:file:docs-source/synchronization-notes.md}
  module Synchronization
  end
end

