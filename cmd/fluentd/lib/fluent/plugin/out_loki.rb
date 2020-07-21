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

      desc 'url of loki server'
      config_param :url, :string, default: 'https://logs-us-west1.grafana.net'

      desc 'BasicAuth credentials'
      config_param :username, :string, default: nil
      config_param :password, :string, default: nil, secret: true

      desc 'Client certificate'
      config_param :cert, :string, default: nil
      config_param :key, :string, default: nil

      desc 'TLS'
      config_param :ca_cert, :string, default: nil

      desc 'Disable server certificate verification'
      config_param :insecure_tls, :bool, default: false

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

      config_section :buffer do
        config_set_default :@type, DEFAULT_BUFFER_TYPE
        config_set_default :chunk_keys, []
      end

      def configure(conf) # rubocop:disable Metrics/CyclomaticComplexity
        compat_parameters_convert(conf, :buffer)
        super
        @uri = URI.parse(@url + '/loki/api/v1/push')
        unless @uri.is_a?(URI::HTTP) || @uri.is_a?(URI::HTTPS)
          raise Fluent::ConfigError, 'url parameter must be valid HTTP'
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

        if ssl_cert?
          load_ssl
          validate_ssl_key
        end

        raise "CA certificate file #{@ca_cert} not found" if !@ca_cert.nil? && !File.exist?(@ca_cert)
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

      def multi_workers_ready?
        true
      end

      def http_opts(uri)
        opts = {
          use_ssl: uri.scheme == 'https'
        }
        opts
      end

      # flush a chunk to loki
      def write(chunk)
        # streams by label
        payload = generic_to_loki(chunk)
        body = { 'streams' => payload }

        tenant = extract_placeholders(@tenant, chunk) if @tenant

        # add ingest path to loki url
        res = loki_http_request(body, tenant)

        return if res.is_a?(Net::HTTPSuccess)

        res_summary = "#{res.code} #{res.message} #{res.body}"
        log.warn "failed to write post to #{@uri} (#{res_summary})"
        log.debug Yajl.dump(body)

        # Only retry 429 and 500s
        raise(LogPostError, res_summary) if res.is_a?(Net::HTTPTooManyRequests) || res.is_a?(Net::HTTPServerError)
      end

      def ssl_opts(uri)
        opts = {
          use_ssl: uri.scheme == 'https'
        }

        # Disable server TLS certificate verification
        if @insecure_tls
          opts = opts.merge(
            verify_mode: OpenSSL::SSL::VERIFY_NONE
          )
        end

        # Verify client TLS certificate
        if !@cert.nil? && !@key.nil?
          opts = opts.merge(
            cert: @cert,
            key: @key
          )
        end

        # Specify custom certificate authority
        unless @ca_cert.nil?
          opts = opts.merge(
            ca_file: @ca_cert
          )
        end
        opts
      end

      def generic_to_loki(chunk)
        # log.debug("GenericToLoki: converting #{chunk}")
        streams = chunk_to_loki(chunk)
        payload = payload_builder(streams)
        payload
      end

      private

      def loki_http_request(body, tenant)
        req = Net::HTTP::Post.new(
          @uri.request_uri
        )
        req.add_field('Content-Type', 'application/json')
        req.add_field('X-Scope-OrgID', tenant) if tenant
        req.body = Yajl.dump(body)
        req.basic_auth(@username, @password) if @username

        opts = ssl_opts(@uri)

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
        data_labels.each { |k, v| formatted_labels[k] = v.gsub('"', '\\"') if v && v&.is_a?(String) }
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
              formatted_labels.push(%(#{k}="#{v}"))
            end
            line = formatted_labels.join(' ')
          end
        end
        line
      end

      #
      # convert a line to loki line with labels
      def line_to_loki(record)
        chunk_labels = {}
        line = ''
        if record.is_a?(Hash)
          @record_accessors&.each do |name, accessor|
            new_key = name.gsub(%r{[.\-\/]}, '_')
            chunk_labels[new_key] = accessor.call(record)
            accessor.delete(record)
          end

          if @extract_kubernetes_labels && record.key?('kubernetes')
            kubernetes_labels = record['kubernetes']['labels']
            kubernetes_labels.each_key do |l|
              new_key = l.gsub(%r{[.\-\/]}, '_')
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
        if @buffer_config.flush_thread_count > 1
          chunk_labels['fluentd_thread'] = Thread.current[:_fluentd_plugin_helper_thread_title].to_s
        end

        # return both the line content plus the labels found in the record
        {
          line: line,
          labels: chunk_labels
        }
      end

      # iterate through each chunk and create a loki stream entry
      def chunk_to_loki(chunk)
        streams = {}
        last_time = nil
        chunk.each do |time, record|
          # each chunk has a unique set of labels
          last_time = time if last_time.nil?
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
