package com.grafana.loki.http;

import java.io.IOException;
import java.net.URI;
import java.util.Base64;

import javax.net.ssl.SSLContext;

import org.apache.http.client.methods.CloseableHttpResponse;
import org.apache.http.client.methods.HttpPost;
import org.apache.http.entity.ByteArrayEntity;
import org.apache.http.impl.client.CloseableHttpClient;
import org.apache.http.impl.client.HttpClients;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.spi.ContextAwareBase;

public class HttpHandler extends ContextAwareBase {
	private static CloseableHttpClient client;
	
	private CloseableHttpResponse response;

	public HttpHandler(Context ctx) {
		this.setContext(ctx);
	}
	

	public CloseableHttpResponse sendLoki(String tenantID, URI uri, byte[] body, SSLContext sslContext,
			String basicAuth) throws IOException {

		if (sslContext != null) {
			client = HttpClients.custom().setSSLContext(sslContext).build();
		} else {
			client = HttpClients.custom().build();
		}

		HttpPost httpPost = new HttpPost();
		httpPost.setURI(uri);
		httpPost.setEntity(new ByteArrayEntity(body));

		httpPost.setHeader("Content-Type", "application/x-protobuf");

		if (basicAuth != null) {
			httpPost.setHeader("Authorization", basicAuth);
		}

		if (tenantID != null) {
			httpPost.setHeader("X-Scope-OrgID", tenantID);
		}

		response = client.execute(httpPost);
		
		return response;

	}

	public String basicAuth(String username, String password) {
		if (username != null && password != null) {
			return "Basic " + Base64.getEncoder().encodeToString((username + ":" + password).getBytes());
		} else {
			return null;
		}
	}
	
	public void closeConnections() throws IOException {
		if(response != null) {
			response.close();
		}
		
		if(client != null) {
			client.close();
		}
	}
}
