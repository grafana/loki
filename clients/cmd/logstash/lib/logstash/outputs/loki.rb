# encoding: utf-8
require "logstash/outputs/base"
require "logstash/outputs/loki/entry"
require "logstash/outputs/loki/batch"
require "logstash/namespace"
require 'net/http'
require 'time'
require 'uri'
require 'json'

class LogStash::Outputs::Loki < LogStash::Outputs::Base
  include Loki
  config_name "loki"

  ## 'A single instance of the Output will be shared among the pipeline worker threads'
  concurrency :single

  ## 'Loki URL'
  config :url, :validate => :string, :required => true

  ## 'BasicAuth credentials'
  config :username, :validate => :string, :required => false
  config :password, :validate => :string, secret: true, :required => false

  ## 'Client certificate'
  config :cert, :validate => :path, :required => false
  config :key, :validate => :path, :required => false

  ## 'TLS'
  config :ca_cert, :validate => :path, :required => false

  ## 'Disable server certificate verification'
  config :insecure_skip_verify, :validate => :boolean, :default => false, :required => false

  ## 'Loki Tenant ID'
  config :tenant_id, :validate => :string, :required => false

  ## 'Maximum batch size to accrue before pushing to loki. Defaults to 102400 bytes'
  config :batch_size, :validate => :number, :default => 102400, :required => false

  ## 'Interval in seconds to wait before pushing a batch of records to loki. Defaults to 1 second'
  config :batch_wait, :validate => :number, :default => 1, :required => false

  ## 'Log line field to pick from logstash. Defaults to "message"'
  config :message_field, :validate => :string, :default => "message", :required => false

  ## 'Backoff configuration. Initial backoff time between retries. Default 1s'
  config :min_delay, :validate => :number, :default => 1, :required => false

  ## 'An array of fields to map to labels, if defined only fields in this list will be mapped.'
  config :include_fields, :validate => :array, :default => [], :required => false

  ## 'An array of fields to map to structure metadata, if defined only fields in this list will be mapped.'
  config :metadata_fields, :validate => :array, :default => [], :required => false

  ## 'Backoff configuration. Maximum backoff time between retries. Default 300s'
  config :max_delay, :validate => :number, :default => 300, :required => false

  ## 'Backoff configuration. Maximum number of retries to do'
  config :retries, :validate => :number, :default => 10, :required => false

  attr_reader :batch
  public
  def register
    @uri = URI.parse(@url)
    unless @uri.is_a?(URI::HTTP) || @uri.is_a?(URI::HTTPS)
      raise LogStash::ConfigurationError, "url parameter must be valid HTTP, currently '#{@url}'"
    end

    if @min_delay > @max_delay
      raise LogStash::ConfigurationError, "Min delay should be less than Max delay, currently 'Min delay is #{@min_delay} and Max delay is #{@max_delay}'"
    end

    @logger.info("Loki output plugin", :class => self.class.name)

    # initialize Queue and Mutex
    @entries = Queue.new
    @mutex = Mutex.new
    @stop = false

    # create nil batch object.
    @batch = nil

    # validate certs
    if ssl_cert?
      load_ssl
      validate_ssl_key
    end

    # start batch_max_wait and batch_max_size threads
    @batch_wait_thread = Thread.new{max_batch_wait()}
    @batch_size_thread = Thread.new{max_batch_size()}
  end

  def max_batch_size
    loop do
      @mutex.synchronize do
        return if @stop
      end

      e = @entries.deq
      return if e.nil?

      @mutex.synchronize do
        if !add_entry_to_batch(e)
          @logger.debug("Max batch_size is reached. Sending batch to loki")
          send(@batch)
          @batch = Batch.new(e)
        end
      end
    end
  end

  def max_batch_wait
    # minimum wait frequency is 10 milliseconds
	  min_wait_checkfrequency = 1/100
	  max_wait_checkfrequency = @batch_wait
	  if max_wait_checkfrequency < min_wait_checkfrequency
		  max_wait_checkfrequency = min_wait_checkfrequency
    end

    loop do
      @mutex.synchronize do
        return if @stop
      end

      sleep(max_wait_checkfrequency)
      if is_batch_expired
        @mutex.synchronize do
          @logger.debug("Max batch_wait time is reached. Sending batch to loki")
          send(@batch)
          @batch = nil
        end
      end
    end
  end

  def ssl_cert?
    !@key.nil? && !@cert.nil?
  end

  def load_ssl
    @cert = OpenSSL::X509::Certificate.new(File.read(@cert)) if @cert
    @key = OpenSSL::PKey.read(File.read(@key)) if @key
  end

  def validate_ssl_key
    if !@key.is_a?(OpenSSL::PKey::RSA) && !@key.is_a?(OpenSSL::PKey::DSA)
      raise LogStash::ConfigurationError, "Unsupported private key type '#{@key.class}''"
    end
  end

  def ssl_opts(uri)
    opts = {
      use_ssl: uri.scheme == 'https'
    }

     # disable server certificate verification
    if @insecure_skip_verify
      opts = opts.merge(
        verify_mode: OpenSSL::SSL::VERIFY_NONE
      )
    end

    if !@cert.nil? && !@key.nil?
      opts = opts.merge(
        verify_mode: OpenSSL::SSL::VERIFY_PEER,
        cert: @cert,
        key: @key
      )
    end

    unless @ca_cert.nil?
      opts = opts.merge(
        ca_file: @ca_cert
      )
    end
    opts
  end

  # Add an entry to the current batch returns false if the batch is full
  # and the entry can't be added.
  def add_entry_to_batch(e)
    line = e.entry['line']
    # we don't want to send empty lines.
    return true if line.to_s.strip.empty?

    if @batch.nil?
      @batch = Batch.new(e)
      return true
    end

    if @batch.size_bytes_after(line) > @batch_size
      return false
    end
    @batch.add(e)
    return true
  end

  def is_batch_expired
    return !@batch.nil? && @batch.age() >= @batch_wait
  end

  ## Receives logstash events
  public
  def receive(event)
    @entries << Entry.new(event, @message_field, @include_fields, @metadata_fields)
  end

  def close
    @entries.close
    @mutex.synchronize do
      @stop = true
    end
    @batch_wait_thread.join
    @batch_size_thread.join

    # if by any chance we still have a forming batch, we need to send it.
    send(@batch) if !@batch.nil?
    @batch = nil
  end

  def send(batch)
    payload = batch.to_json
    res = loki_http_request(payload)
    if res.is_a?(Net::HTTPSuccess)
      @logger.debug("Successfully pushed data to loki")
    else
      @logger.debug("failed payload", :payload => payload)
    end
  end

  def loki_http_request(payload)
    req = Net::HTTP::Post.new(
      @uri.request_uri
    )
    req.add_field('Content-Type', 'application/json')
    req.add_field('X-Scope-OrgID', @tenant_id) if @tenant_id
    req['User-Agent']= 'loki-logstash'
    req.basic_auth(@username, @password) if @username
    req.body = payload

    opts = ssl_opts(@uri)

    @logger.debug("sending #{req.body.length} bytes to loki")
    retry_count = 0
    delay = @min_delay
    begin
      res = Net::HTTP.start(@uri.host, @uri.port, **opts) { |http|
        http.request(req)
      }
      return res if !res.nil? && res.code.to_i != 429 && res.code.to_i.div(100) != 5
      raise StandardError.new res
    rescue StandardError => e
      retry_count += 1
      @logger.warn("Failed to send batch, attempt: #{retry_count}/#{@retries}", :error_inspect => e.inspect, :error => e)
      if retry_count < @retries
        sleep delay
        if delay * 2 <= @max_delay
          delay = delay * 2
        else
          delay = @max_delay
        end
        retry
      else
        @logger.error("Failed to send batch", :error_inspect => e.inspect, :error => e)
        return res
      end
    end
  end
end
