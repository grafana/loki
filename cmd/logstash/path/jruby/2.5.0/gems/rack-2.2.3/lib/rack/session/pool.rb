# frozen_string_literal: true

# AUTHOR: blink <blinketje@gmail.com>; blink#ruby-lang@irc.freenode.net
# THANKS:
#   apeiros, for session id generation, expiry setup, and threadiness
#   sergio, threadiness and bugreps

require_relative 'abstract/id'
require 'thread'

module Rack
  module Session
    # Rack::Session::Pool provides simple cookie based session management.
    # Session data is stored in a hash held by @pool.
    # In the context of a multithreaded environment, sessions being
    # committed to the pool is done in a merging manner.
    #
    # The :drop option is available in rack.session.options if you wish to
    # explicitly remove the session from the session cache.
    #
    # Example:
    #   myapp = MyRackApp.new
    #   sessioned = Rack::Session::Pool.new(myapp,
    #     :domain => 'foo.com',
    #     :expire_after => 2592000
    #   )
    #   Rack::Handler::WEBrick.run sessioned

    class Pool < Abstract::PersistedSecure
      attr_reader :mutex, :pool
      DEFAULT_OPTIONS = Abstract::ID::DEFAULT_OPTIONS.merge drop: false

      def initialize(app, options = {})
        super
        @pool = Hash.new
        @mutex = Mutex.new
      end

      def generate_sid
        loop do
          sid = super
          break sid unless @pool.key? sid.private_id
        end
      end

      def find_session(req, sid)
        with_lock(req) do
          unless sid and session = get_session_with_fallback(sid)
            sid, session = generate_sid, {}
            @pool.store sid.private_id, session
          end
          [sid, session]
        end
      end

      def write_session(req, session_id, new_session, options)
        with_lock(req) do
          @pool.store session_id.private_id, new_session
          session_id
        end
      end

      def delete_session(req, session_id, options)
        with_lock(req) do
          @pool.delete(session_id.public_id)
          @pool.delete(session_id.private_id)
          generate_sid unless options[:drop]
        end
      end

      def with_lock(req)
        @mutex.lock if req.multithread?
        yield
      ensure
        @mutex.unlock if @mutex.locked?
      end

      private

      def get_session_with_fallback(sid)
        @pool[sid.private_id] || @pool[sid.public_id]
      end
    end
  end
end
