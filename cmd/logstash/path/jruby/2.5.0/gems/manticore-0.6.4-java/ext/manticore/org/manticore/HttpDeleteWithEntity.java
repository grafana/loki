package org.manticore;

import java.net.URI;
import org.apache.http.client.methods.HttpEntityEnclosingRequestBase;

public class HttpDeleteWithEntity extends HttpEntityEnclosingRequestBase {
    public final static String METHOD_NAME = "DELETE";

    public HttpDeleteWithEntity() {
        super();
    }

    public HttpDeleteWithEntity(URI url) {
        super();
        setURI(url);
    }

    public HttpDeleteWithEntity(String url) {
        super();
        setURI(URI.create(url));
    }

    @Override
    public String getMethod() {
        return METHOD_NAME;
    }
}