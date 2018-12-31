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

require "fluent/plugin/output"
require 'net/http'
require 'uri'
require 'yajl'
require 'time'

module Fluent
  module Plugin
    class LokiOutput < Fluent::Plugin::Output
      Fluent::Plugin.register_output("loki", self)

      helpers :compat_parameters

      DEFAULT_BUFFER_TYPE = "memory"

      # url of loki server
      config_param :url, :string, :default => 'https://logs-us-west1.grafana.net'

      # BasicAuth credentials
      config_param :username, :string, :default => nil
      config_param :password, :string, :default => nil, :secret => true

      # Loki tenant id
      config_param :tenant, :string, :default => nil

      # extra labels to add to all log streams
      config_param :labels, :hash, :default => nil

      config_section :buffer do
        config_set_default :@type, DEFAULT_BUFFER_TYPE
        config_set_default :chunk_keys, []
      end


      def configure(conf)
        compat_parameters_convert(conf, :buffer)
        super
      end

      def http_opts(uri)
        opts = {
          :use_ssl => uri.scheme == 'https'
        }
        opts
      end
  
      # flush a chunk to loki
      def write(chunk)
        # streams by label
        streams = {}

        # iterate through each chunk.  If the record has a "message" field then we use that as the "line" for the Loki
        # stream entry.  Else the just set "line" to the stringified record.
        chunk.each do | time, record |
          line = ""
          lbl = @labels
          if record.key?('message')
            line = record['message']
            record.each do | k, v |
              lbl[k] = v if k != 'message'
            end
          else
            line = record.to_s
          end
          if streams[lbl] == nil
            streams[lbl] = []
          end
          streams[lbl].push({'ts' => Time.at(time.to_int).iso8601, 'line' => line})
        end

        # create the request body we will send to Loki.  For now, we will send using the JSON format, but
        # for performance we should convert this to use protobufs
        body = {"streams" => []}
        streams.each do | k, v |
          # create a stream for each label set.  We additionally sort the entries by timestamp just incase we
          # got them out of order.
          body['streams'].push({'labels' => Yajl.dump(k)}, 'entries' => v.sort_by{ |hsh| hsh[:ts] })
        end

        # send the streams to loki
        File.open('/tmp/loki.out', 'w') { |file| file.write(Yajl.dump(body)) }
        uri = URI.parse(url+"/api/prom/push")
        req = Net::HTTP::Post.new(uri.request_uri)
        req.body = Yajl.dump(body)
        req['Content-Type'] = 'application/json'
        if @tenant
          req["X-Scope-OrgID"] = @tenant
        end
        if @username
          req.basic_auth(@username, @password)
        end
        opts = {
          :use_ssl => uri.scheme == 'https'
        }
        res = Net::HTTP.start(uri.host, uri.port, **opts) {|http| http.request(req) }
        unless res and res.is_a?(Net::HTTPSuccess)
          res_summary = if res
                           "#{res.code} #{res.message} #{res.body}"
                        else
                           "res=nil"
                        end
          log.warn "failed to #{req.method} #{uri} (#{res_summary})"
        end
      end
    end
  end
end
