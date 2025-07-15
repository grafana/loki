# frozen_string_literal: true

#
# Copyright 2018- Grafana Labs
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

require 'fluent/env'
require 'fluent/plugin/output'
require 'net/http'
require 'yajl'
require 'time'

module Fluent
  module Plugin
    # Subclass of Fluent Plugin Output
    class LokiOutput < Fluent::Plugin::Output # rubocop:disable Metrics/ClassLength
      Fluent::Plugin.register_output('loki', self)

      class LogPostError < StandardError; end

      helpers :compat_parameters, :record_accessor

      attr_accessor :record_accessors

      DEFAULT_BUFFER_TYPE = 'memory'

      desc 'Loki API base URL'
      config_param :url, :string, default: 'https://logs-prod-us-central1.grafana.net'

      desc 'Authentication: basic auth credentials'
      config_param :username, :string, default: nil
      config_param :password, :string, default: nil, secret: true

      desc 'Authentication: Authorization header with Bearer token scheme'
      config_param :bearer_token_file, :string, default: nil

      desc 'TLS: parameters for presenting a client certificate'
      config_param :cert, :string, default: nil
      config_param :key, :string, default: nil

      desc 'TLS: CA certificate file for server certificate verification'
      config_param :ca_cert, :string, default: nil

      desc 'TLS: the ciphers to use for the tls connection (e.g TLS1_0, TLS1_1, TLS1_2)'
      config_param :ciphers, :string, default: nil

      desc 'TLS: The minimum version for the tls connection'
      config_param :min_version, :string, default: nil

      desc 'TLS: disable server certificate verification'
      config_param :insecure_tls, :bool, default: false

      desc 'Custom HTTP headers'
      config_param :custom_headers, :hash, default: {}

      desc 'Loki tenant id'
      config_param :tenant, :string, default: nil

      desc 'extra labels to add to all log streams'
      config_param :extra_labels, :hash, default: {}

      desc 'format to use when flattening the record to a log line'
      config_param :line_format, :enum, list: %i[json key_value], default: :key_value

      desc 'extract kubernetes labels as loki labels'
      config_param :extract_kubernetes_labels, :bool, default: false

      desc 'comma separated list of needless record keys to remove'
      config_param :remove_keys, :array, default: %w[], value_type: :string

      desc 'if a record only has 1 key, then just set the log line to the value and discard the key.'
      config_param :drop_single_key, :bool, default: false

      desc 'whether or not to include the fluentd_thread label when multiple threads are used for flushing'
      config_param :include_thread_label, :bool, default: true

      config_section :buffer do
        config_set_default :@type, DEFAULT_BUFFER_TYPE
        config_set_default :chunk_keys, []
      end

      # rubocop:disable Metrics/AbcSize, Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity
      def configure(conf)
        compat_parameters_convert(conf, :buffer)
        super
        @uri = URI.parse("#{@url}/loki/api/v1/push")
        unless @uri.is_a?(URI::HTTP) || @uri.is_a?(URI::HTTPS)
          raise Fluent::ConfigError, 'URL parameter must have HTTP/HTTPS scheme'
        end

        @record_accessors = {}
        conf.elements.select { |element| element.name == 'label' }.each do |element|
          element.each_pair do |k, v|
            element.has_key?(k) # rubocop:disable Style/PreferredHashMethods #to suppress unread configuration warning
            v = k if v.empty?
            @record_accessors[k] = record_accessor_create(v)
          end
        end
        @remove_keys_accessors = []
        @remove_keys.each do |key|
          @remove_keys_accessors.push(record_accessor_create(key))
        end

        # If configured, load and validate client certificate (and corresponding key)
        if client_cert_configured?
          load_client_cert
          validate_client_cert_key
        end

        if !@bearer_token_file.nil? && !File.exist?(@bearer_token_file)
          raise "bearer_token_file #{@bearer_token_file} not found"
        end

        @auth_token_bearer = nil
        unless @bearer_token_file.nil?
          raise "bearer_token_file #{@bearer_token_file} not found" unless File.exist?(@bearer_token_file)

          # Read the file once, assume long-lived authentication token.
          @auth_token_bearer = File.read(@bearer_token_file)
          raise "bearer_token_file #{@bearer_token_file} is empty" if @auth_token_bearer.empty?

          log.info "will use Bearer token from bearer_token_file #{@bearer_token_file} in Authorization header"
        end

        raise "CA certificate file #{@ca_cert} not found" if !@ca_cert.nil? && !File.exist?(@ca_cert)
      end
      # rubocop:enable Metrics/AbcSize, Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity

      def client_cert_configured?
        !@key.nil? && !@cert.nil?
      end

      def load_client_cert
        @cert = OpenSSL::X509::Certificate.new(File.read(@cert)) if @cert
        @key = OpenSSL::PKey.read(File.read(@key)) if @key
      end

      def validate_client_cert_key
        if !@key.is_a?(OpenSSL::PKey::RSA) && !@key.is_a?(OpenSSL::PKey::DSA)
          raise "Unsupported private key type #{key.class}"
        end
      end

      def multi_workers_ready?
        true
      end

      # flush a chunk to loki
      def write(chunk)
        # streams by label
        payload = generic_to_loki(chunk)
        body = { 'streams' => payload }

        tenant = extract_placeholders(@tenant, chunk) if @tenant

        # add ingest path to loki url
        res = loki_http_request(body, tenant)

        if res.is_a?(Net::HTTPSuccess)
          log.debug "POST request was responded to with status code #{res.code}"
          return
        end

        res_summary = "#{res.code} #{res.message} #{res.body}"
        log.warn "failed to write post to #{@uri} (#{res_summary})"
        log.debug Yajl.dump(body)

        # Only retry 429 and 500s
        raise(LogPostError, res_summary) if res.is_a?(Net::HTTPTooManyRequests) || res.is_a?(Net::HTTPServerError)
      end

      def http_request_opts(uri)
        opts = {
          use_ssl: uri.scheme == 'https'
        }

        # Optionally disable server server certificate verification.
        if @insecure_tls
          opts = opts.merge(
            verify_mode: OpenSSL::SSL::VERIFY_NONE
          )
        end

        # Optionally present client certificate
        if !@cert.nil? && !@key.nil?
          opts = opts.merge(
            cert: @cert,
            key: @key
          )
        end

        # For server certificate verification: set custom CA bundle.
        # Only takes effect when `insecure_tls` is not set.
        unless @ca_cert.nil?
          opts = opts.merge(
            ca_file: @ca_cert
          )
        end

        if @ciphers
          opts = opts.merge(
            ciphers: @ciphers
          )
        end

        if @min_version
          opts = opts.merge(
            min_version: @min_version.to_sym
          )
        end

        opts
      end

      def generic_to_loki(chunk)
        # log.debug("GenericToLoki: converting #{chunk}")
        streams = chunk_to_loki(chunk)
        payload_builder(streams)
      end

      private

      def loki_http_request(body, tenant)
        req = Net::HTTP::Post.new(
          @uri.request_uri
        )
        @custom_headers.each do |key, value|
          req.add_field(key, value)
        end
        req.add_field('Content-Type', 'application/json')
        req.add_field('Authorization', "Bearer #{@auth_token_bearer}") unless @auth_token_bearer.nil?
        req.add_field('X-Scope-OrgID', tenant) if tenant
        req.body = Yajl.dump(body)
        req.basic_auth(@username, @password) if @username

        opts = http_request_opts(@uri)

        msg = "sending #{req.body.length} bytes to loki"
        msg += " (tenant: \"#{tenant}\")" if tenant
        log.debug msg

        Net::HTTP.start(@uri.host, @uri.port, **opts) { |http| http.request(req) }
      end

      def numeric?(val)
        !Float(val).nil?
      rescue StandardError
        false
      end

      def format_labels(data_labels)
        formatted_labels = {}
        # merge extra_labels with data_labels. If there are collisions extra_labels win.
        data_labels = {} if data_labels.nil?
        data_labels = data_labels.merge(@extra_labels)
        # sanitize label values
        data_labels.each { |k, v| formatted_labels[k] = v.gsub('"', '\\"') if v.is_a?(String) }
        formatted_labels
      end

      def payload_builder(streams)
        payload = []
        streams.each do |k, v|
          # create a stream for each label set.
          # Additionally sort the entries by timestamp just in case we
          # got them out of order.
          entries = v.sort_by.with_index { |hsh, i| [hsh['ts'], i] }
          payload.push(
            'stream' => format_labels(k),
            'values' => entries.map { |e| [e['ts'].to_s, e['line']] }
          )
        end
        payload
      end

      def to_nano(time)
        # time is a Fluent::EventTime object, or an Integer which represents unix timestamp (seconds from Epoch)
        # https://docs.fluentd.org/plugin-development/api-plugin-output#chunk-each-and-block
        if time.is_a?(Fluent::EventTime)
          time.to_i * (10**9) + time.nsec
        else
          time.to_i * (10**9)
        end
      end

      # rubocop:disable Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity
      def record_to_line(record)
        line = ''
        if @drop_single_key && record.keys.length == 1
          line = record[record.keys[0]]
        else
          case @line_format
          when :json
            line = Yajl.dump(record)
          when :key_value
            formatted_labels = []
            record.each do |k, v|
              # Remove non UTF-8 characters by force-encoding the string
              v = v.encode('utf-8', invalid: :replace, undef: :replace, replace: '?') if v.is_a?(String)
              # Escape double quotes and backslashes by prefixing them with a backslash
              v = v.to_s.gsub(/(["\\])/, '\\\\\1')
              if v.include?(' ') || v.include?('=')
                formatted_labels.push(%(#{k}="#{v}"))
              else
                formatted_labels.push(%(#{k}=#{v}))
              end
            end
            line = formatted_labels.join(' ')
          end
        end
        line
      end
      # rubocop:enable Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity

      # convert a line to loki line with labels
      # rubocop:disable Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity
      def line_to_loki(record)
        chunk_labels = {}
        line = ''
        if record.is_a?(Hash)
          @record_accessors&.each do |name, accessor|
            new_key = name.gsub(%r{[.\-/]}, '_')
            chunk_labels[new_key] = accessor.call(record)
            accessor.delete(record)
          end

          if @extract_kubernetes_labels && record.key?('kubernetes')
            kubernetes_labels = record['kubernetes']['labels']
            kubernetes_labels&.each_key do |l|
              new_key = l.gsub(%r{[.\-/]}, '_')
              chunk_labels[new_key] = kubernetes_labels[l]
            end
          end

          # remove needless keys.
          @remove_keys_accessors&.each do |deleter|
            deleter.delete(record)
          end

          line = record_to_line(record)
        else
          line = record.to_s
        end

        # add buffer flush thread title as a label if there are multiple flush threads
        # this prevents "entry out of order" errors in loki by making the label constellation
        # unique per flush thread
        # note that flush thread != fluentd worker. if you use multiple workers you still need to
        # add the worker id as a label
        if @include_thread_label && @buffer_config.flush_thread_count > 1
          chunk_labels['fluentd_thread'] = Thread.current[:_fluentd_plugin_helper_thread_title].to_s
        end

        # return both the line content plus the labels found in the record
        {
          line: line,
          labels: chunk_labels
        }
      end
      # rubocop:enable Metrics/CyclomaticComplexity, Metrics/PerceivedComplexity

      # iterate through each chunk and create a loki stream entry
      def chunk_to_loki(chunk)
        streams = {}
        chunk.each do |time, record|
          # each chunk has a unique set of labels
          result = line_to_loki(record)
          chunk_labels = result[:labels]
          # initialize a new stream with the chunk_labels if it does not exist
          streams[chunk_labels] = [] if streams[chunk_labels].nil?
          # NOTE: timestamp must include nanoseconds
          # append to matching chunk_labels key
          streams[chunk_labels].push(
            'ts' => to_nano(time),
            'line' => result[:line]
          )
        end
        streams
      end
    end
  end
end
