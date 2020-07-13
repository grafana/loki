java_import "org.apache.http.client.methods.HttpRequestBase"

class Java::OrgApacheHttpClientMethods::HttpRequestBase

  # Provides an easy way to get the request headers from any request
  def headers
    Hash[*get_all_headers.flat_map { |h| [h.name, h.value] }]
  end

  # Get a single request header
  def [](key)
    h = get_last_header(key)
    h && h.value || nil
  end

  # Set a single request header
  def []=(key, val)
    set_header key, val
  end
end
