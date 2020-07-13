# encoding: utf-8
require "logstash/codecs/base"
require "logstash/util/charset"

# The "plain" codec is for plain text with no delimiting between events.
#
# This is mainly useful on inputs and outputs that already have a defined
# framing in their transport protocol (such as zeromq, rabbitmq, redis, etc)
class LogStash::Codecs::Plain < LogStash::Codecs::Base
  config_name "plain"

  # Set the message you which to emit for each event. This supports `sprintf`
  # strings.
  #
  # This setting only affects outputs (encoding of events).
  config :format, :validate => :string

  # The character encoding used in this input. Examples include `UTF-8`
  # and `cp1252`
  #
  # This setting is useful if your log files are in `Latin-1` (aka `cp1252`)
  # or in another character set other than `UTF-8`.
  #
  # This only affects "plain" format logs since json is `UTF-8` already.
  config :charset, :validate => ::Encoding.name_list, :default => "UTF-8"

  MESSAGE_FIELD = "message".freeze

  def register
    @converter = LogStash::Util::Charset.new(@charset)
    @converter.logger = @logger
  end

  def decode(data)
    yield LogStash::Event.new(MESSAGE_FIELD => @converter.convert(data))
  end

  def encode(event)
    encoded = @format ? event.sprintf(@format) : event.to_s
    @on_event.call(event, encoded)
  end
end
