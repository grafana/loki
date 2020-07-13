# frozen_string_literal: true

require 'rack/lobster'

use Rack::ShowExceptions
run Rack::Lobster.new
