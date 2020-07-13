# frozen_string_literal: true

require 'rack/session/dalli'

module Rack
  module Session
    warn "Rack::Session::Memcache is deprecated, please use Rack::Session::Dalli from 'dalli' gem instead."
    Memcache = Dalli
  end
end
