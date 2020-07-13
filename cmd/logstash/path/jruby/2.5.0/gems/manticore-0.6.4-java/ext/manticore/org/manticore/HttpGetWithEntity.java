package org.manticore;

import java.net.URI;
import org.apache.http.client.methods.HttpEntityEnclosingRequestBase;

public class HttpGetWithEntity extends HttpEntityEnclosingRequestBase {
    public final static String METHOD_NAME = "GET";

    public HttpGetWithEntity() {
        super();
    }

    public HttpGetWithEntity(URI url) {
        super();
        setURI(url);
    }

    public HttpGetWithEntity(String url) {
        super();
        setURI(URI.create(url));
    }

    @Override
    public String getMethod() {
        return METHOD_NAME;
    }
}