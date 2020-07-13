require 'concurrent'

require 'concurrent/edge/version'

require 'concurrent/actor'
require 'concurrent/agent'
require 'concurrent/channel'
require 'concurrent/lazy_register'
require 'concurrent/executor/wrapping_executor' if Concurrent.ruby_version :>=, 2, 1, 0

require 'concurrent/edge/lock_free_linked_set'
require 'concurrent/edge/lock_free_queue'

require 'concurrent/edge/cancellation'
require 'concurrent/edge/throttle'
require 'concurrent/edge/channel'

require 'concurrent/edge/processing_actor'
require 'concurrent/edge/erlang_actor' if Concurrent.ruby_version :>=, 2, 1, 0
