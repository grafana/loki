# frozen_string_literal: true

# Copyright (C) 2007-2019 Leah Neukirchen <http://leahneukirchen.org/infopage.html>
#
# Rack is freely distributable under the terms of an MIT-style license.
# See MIT-LICENSE or https://opensource.org/licenses/MIT.

# The Rack main module, serving as a namespace for all core Rack
# modules and classes.
#
# All modules meant for use in your application are <tt>autoload</tt>ed here,
# so it should be enough just to <tt>require 'rack'</tt> in your code.

require_relative 'rack/version'

module Rack
  HTTP_HOST         = 'HTTP_HOST'
  HTTP_PORT         = 'HTTP_PORT'
  HTTP_VERSION      = 'HTTP_VERSION'
  HTTPS             = 'HTTPS'
  PATH_INFO         = 'PATH_INFO'
  REQUEST_METHOD    = 'REQUEST_METHOD'
  REQUEST_PATH      = 'REQUEST_PATH'
  SCRIPT_NAME       = 'SCRIPT_NAME'
  QUERY_STRING      = 'QUERY_STRING'
  SERVER_PROTOCOL   = 'SERVER_PROTOCOL'
  SERVER_NAME       = 'SERVER_NAME'
  SERVER_PORT       = 'SERVER_PORT'
  CACHE_CONTROL     = 'Cache-Control'
  EXPIRES           = 'Expires'
  CONTENT_LENGTH    = 'Content-Length'
  CONTENT_TYPE      = 'Content-Type'
  SET_COOKIE        = 'Set-Cookie'
  TRANSFER_ENCODING = 'Transfer-Encoding'
  HTTP_COOKIE       = 'HTTP_COOKIE'
  ETAG              = 'ETag'

  # HTTP method verbs
  GET     = 'GET'
  POST    = 'POST'
  PUT     = 'PUT'
  PATCH   = 'PATCH'
  DELETE  = 'DELETE'
  HEAD    = 'HEAD'
  OPTIONS = 'OPTIONS'
  LINK    = 'LINK'
  UNLINK  = 'UNLINK'
  TRACE   = 'TRACE'

  # Rack environment variables
  RACK_VERSION                        = 'rack.version'
  RACK_TEMPFILES                      = 'rack.tempfiles'
  RACK_ERRORS                         = 'rack.errors'
  RACK_LOGGER                         = 'rack.logger'
  RACK_INPUT                          = 'rack.input'
  RACK_SESSION                        = 'rack.session'
  RACK_SESSION_OPTIONS                = 'rack.session.options'
  RACK_SHOWSTATUS_DETAIL              = 'rack.showstatus.detail'
  RACK_MULTITHREAD                    = 'rack.multithread'
  RACK_MULTIPROCESS                   = 'rack.multiprocess'
  RACK_RUNONCE                        = 'rack.run_once'
  RACK_URL_SCHEME                     = 'rack.url_scheme'
  RACK_HIJACK                         = 'rack.hijack'
  RACK_IS_HIJACK                      = 'rack.hijack?'
  RACK_HIJACK_IO                      = 'rack.hijack_io'
  RACK_RECURSIVE_INCLUDE              = 'rack.recursive.include'
  RACK_MULTIPART_BUFFER_SIZE          = 'rack.multipart.buffer_size'
  RACK_MULTIPART_TEMPFILE_FACTORY     = 'rack.multipart.tempfile_factory'
  RACK_REQUEST_FORM_INPUT             = 'rack.request.form_input'
  RACK_REQUEST_FORM_HASH              = 'rack.request.form_hash'
  RACK_REQUEST_FORM_VARS              = 'rack.request.form_vars'
  RACK_REQUEST_COOKIE_HASH            = 'rack.request.cookie_hash'
  RACK_REQUEST_COOKIE_STRING          = 'rack.request.cookie_string'
  RACK_REQUEST_QUERY_HASH             = 'rack.request.query_hash'
  RACK_REQUEST_QUERY_STRING           = 'rack.request.query_string'
  RACK_METHODOVERRIDE_ORIGINAL_METHOD = 'rack.methodoverride.original_method'
  RACK_SESSION_UNPACKED_COOKIE_DATA   = 'rack.session.unpacked_cookie_data'

  autoload :Builder, "rack/builder"
  autoload :BodyProxy, "rack/body_proxy"
  autoload :Cascade, "rack/cascade"
  autoload :Chunked, "rack/chunked"
  autoload :CommonLogger, "rack/common_logger"
  autoload :ConditionalGet, "rack/conditional_get"
  autoload :Config, "rack/config"
  autoload :ContentLength, "rack/content_length"
  autoload :ContentType, "rack/content_type"
  autoload :ETag, "rack/etag"
  autoload :Events, "rack/events"
  autoload :File, "rack/file"
  autoload :Files, "rack/files"
  autoload :Deflater, "rack/deflater"
  autoload :Directory, "rack/directory"
  autoload :ForwardRequest, "rack/recursive"
  autoload :Handler, "rack/handler"
  autoload :Head, "rack/head"
  autoload :Lint, "rack/lint"
  autoload :Lock, "rack/lock"
  autoload :Logger, "rack/logger"
  autoload :MediaType, "rack/media_type"
  autoload :MethodOverride, "rack/method_override"
  autoload :Mime, "rack/mime"
  autoload :NullLogger, "rack/null_logger"
  autoload :Recursive, "rack/recursive"
  autoload :Reloader, "rack/reloader"
  autoload :RewindableInput, "rack/rewindable_input"
  autoload :Runtime, "rack/runtime"
  autoload :Sendfile, "rack/sendfile"
  autoload :Server, "rack/server"
  autoload :ShowExceptions, "rack/show_exceptions"
  autoload :ShowStatus, "rack/show_status"
  autoload :Static, "rack/static"
  autoload :TempfileReaper, "rack/tempfile_reaper"
  autoload :URLMap, "rack/urlmap"
  autoload :Utils, "rack/utils"
  autoload :Multipart, "rack/multipart"

  autoload :MockRequest, "rack/mock"
  autoload :MockResponse, "rack/mock"

  autoload :Request, "rack/request"
  autoload :Response, "rack/response"

  module Auth
    autoload :Basic, "rack/auth/basic"
    autoload :AbstractRequest, "rack/auth/abstract/request"
    autoload :AbstractHandler, "rack/auth/abstract/handler"
    module Digest
      autoload :MD5, "rack/auth/digest/md5"
      autoload :Nonce, "rack/auth/digest/nonce"
      autoload :Params, "rack/auth/digest/params"
      autoload :Request, "rack/auth/digest/request"
    end
  end

  module Session
    autoload :Cookie, "rack/session/cookie"
    autoload :Pool, "rack/session/pool"
    autoload :Memcache, "rack/session/memcache"
  end
end
