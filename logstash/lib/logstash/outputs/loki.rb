# encoding: utf-8
require "logstash/outputs/base"
require "logstash/namespace"
require 'net/http'
require 'concurrent-edge'
require 'time'
require 'uri'
require 'json'

# An example output that does nothing.
class LogStash::Outputs::Loki < LogStash::Outputs::Base
  require 'logstash/outputs/loki/batch'
  require 'logstash/outputs/loki/entry'

  config_name "loki"

  ## 'A single instance of the Output will be shared among the pipeline worker threads'
  concurrency :single

  ## 'Loki URL'
  config :url, :validate => :string,  :required => true

  ## 'BasicAuth credentials'
  config :username, :validate => :string, :required => false
  config :password, :validate => :string, secret: true, :required => false

  ## 'Client certificate'
  config :cert, :validate => :path, :required => false
  config :key, :validate => :path, :required => false

  ## 'TLS'
  config :ca_cert, :validate => :path, :required => false

  ## 'Loki Tenant ID'
  config :tenant_id, :validate => :string, :default => "fake", :required => false

  ## 'Maximum batch size to accrue before pushing to loki. Defaults to 102400 bytes'
  config :batch_size, :validate => :number, :default => 102400, :required => false

  ## 'Interval in seconds to wait before pushing a batch of records to loki. Defaults to 1 second'
  config :batch_wait, :validate => :number, :default => 1, :required => false

  ## 'Array of label names to include in all logstreams'
  config :include_labels, :validate => :array, :default => [], :required => true 

  ## 'Array of label names to exclude from sending to loki'
  config :exclude_labels, :validate => :array, :default => [], :required => false 

  ## 'Extra labels to add to all log streams'
  config :external_labels, :valide => :hash,  :default => {}, :required => false

  ## 'Log line field to pick from logstash. Defaults to "message"'
  config :message_field, :validate => :string, :default => "message", :required => false

  public
  def register
    @uri = URI.parse(@url + '/loki/api/v1/push')
    unless @uri.is_a?(URI::HTTP) || @uri.is_a?(URI::HTTPS)
      raise LogStash::ConfigurationError, "url parameter must be valid HTTP, currently '#{@url}'"
    end

    @logger.info("Loki output plugin", :class => self.class.name)

    @Channel = Concurrent::Channel
    @entries = @Channel.new
    @quit = @Channel.new(capacity: 1)
    @exclude_labels += ["message", "@timestamp"] 
    @max_wait_check
    @retry_http = 3
    @delay = 1
    @batch = nil

    if ssl_cert?
      load_ssl
      validate_ssl_key
    end

    @Channel.go{run()}
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
      raise "Unsupported private key type #{key.class}"
    end
  end

  def ssl_opts(uri)
    opts = {
      use_ssl: uri.scheme == 'https'
    }

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

  def run()
	  min_wait_checkfrequency = 1/1000 #1 millisecond
	  max_wait_checkfrequency = @batch_wait
	  if max_wait_checkfrequency < min_wait_checkfrequency
		  max_wait_checkfrequency = min_wait_checkfrequency
    end

    @max_wait_check = Concurrent::Channel.tick(max_wait_checkfrequency)
    loop do
      Concurrent::Channel.select do |s|
        s.take(@quit) {
          return
        }
        s.take(@entries) { |e|
          if @batch.nil?
            @batch = Batch.new(e)
            next
          end
          
          line = e.entry['line']
          if @batch.size_bytes_after(line) > @batch_size
            @logger.debug("Max batch_size is reached. Sending batch to loki")
            send(@tenant_id, @batch)
            @batch = Batch.new(e)
            next
          end
          @batch.add(e)
        }
        s.take(@max_wait_check) {
          # Send batch if max wait time has been reached
          if !@batch.nil? && @batch.age() < @batch_wait
            next
          end
          @logger.debug("Max batch_wait time is reached. Sending batch to loki")
          send(@tenant_id, @batch) if !@batch.nil?
          @batch = nil
        }
      end
    end 
  end

  ## Receives logstash events
  public
  def receive(event)
    labels = {}

    event_hash = event.to_hash
    lbls = extract_labels(event_hash, labels, "")
    data_labels, entry_hash = build_entry(lbls, event)

    @entries << Entry.new(data_labels, entry_hash)
  end

  def build_entry(lbls, event)
    labels = lbls.merge(@external_labels)
    entry_hash = {
      "ts" => event.get("@timestamp").to_i * (10**9),
      "line" => event.get(@message_field).to_s
    }
    return labels, entry_hash
  end 

  def format_key(key)
    if key[0] == "@"
      key = key[1..-1]
    end
    return key
  end 

  def extract_labels(event_hash, labels, parent_key)
    event_hash.each{ |key,value|
      if !@exclude_labels.include?(key)
        key = format_key(key)
        if value.is_a?(Hash)
          if parent_key != ""
            extract_labels(value, labels, parent_key + "_" + key)
          else  
            extract_labels(value, labels, key)
          end
        else
          if parent_key != ""
            labels[parent_key + "_" + key] = value.to_s
          else 
            labels[key] = value.to_s
          end
        end
      end
    }
    return labels
  end

  def close
    @logger.info("Closing loki output plugin. Flushing all pending batches")
    send(@tenant_id, @batch) if !@batch.nil?
    @quit << "quit" 
    @entries.close
    @max_wait_check.close if !@max_wait_check.nil?
  end

  def send(tenant_id, batch)
    payload = build_payload(batch)
    res = loki_http_request(tenant_id, payload)

    if res.is_a?(Net::HTTPSuccess) || res.nil?
      @logger.debug("Successfully pushed data to loki")
      return
    else  
      @logger.warn("failed to write post to ", :uri => @uri, :code => res.code, :body => res.body, :message => res.message)
      @logger.debug("Payload object ", :payload => payload)
    end 
  end

  def loki_http_request(tenant_id, payload)
    req = Net::HTTP::Post.new(
      @uri.request_uri
    )
    req.add_field('Content-Type', 'application/json')
    req.add_field('X-Scope-OrgID', tenant_id)
    req.basic_auth(@username, @password) if @username
    req.body = payload

    opts = ssl_opts(@uri)
    @logger.debug("sending #{req.body.length} bytes to loki")
    begin 
      res = Net::HTTP.start(@uri.host, @uri.port, **opts) { |http| http.request(req) }
    rescue Net::HTTPTooManyRequests, Net::HTTPServerError, Errno::ECONNREFUSED => e  
      retry_count += 1
      unless @retry_http == -1 || retry_count <= @retry_http
        raise StandardError, "Error while sending data to loki. Tried #{@retry_http - 1} times\n. :error => #{e}"
      end
      @logger.warn("Trying to send again. Retrying in #{@delay}s")
      @delay = @delay * 2
      sleep @delay
      retry 
    rescue StandardError => e
      @logger.error("Error while connecting to loki server ", :error_inspect => e.inspect, :error => e)
    end
    return res
  end 

  def build_payload(batch)
    payload = {}
    payload['streams'] = []
    batch.streams.each { |labels, stream|
        stream_obj = get_stream_obj(stream)
        payload['streams'].push(stream_obj)
    }
    return payload.to_json
  end

  def get_stream_obj(stream)
    stream_obj = {} 
    stream_obj['stream'] = stream['labels']
    stream_obj['values'] = [] 
    values = []
    stream['entries'].each { |entry|
        values.push(entry['ts'].to_s)
        values.push(entry['line'])
    }
    stream_obj['values'].push(values)
    return stream_obj
  end 
end
