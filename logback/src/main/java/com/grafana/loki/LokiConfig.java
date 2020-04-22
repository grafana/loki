package com.grafana.loki;

import java.net.URI;
import java.util.HashMap;
import java.util.Map;

import javax.net.ssl.SSLContext;

public class LokiConfig{
	private String url;
	
	private String tenantID;
	
	private long batchWaitSeconds;
	
	private int batchSizeBytes;
	
	private Map<String, String> labels;
	
	private int maxRetries;
	
	private long minDelaySeconds;
	
	private URI lokiURL;
	
	private String username;
	
	private String password;
	
	private SSLContext sslContext;
	
	public SSLContext getSSLContext() {
		return sslContext;
	}

	public void setSSLContext(SSLContext sslContext) {
		this.sslContext = sslContext;
	}
	
	public String getUsername() {
		return username;
	}

	public void setUsername(String username) {
		this.username = username;
	}

	public String getPassword() {
		return password;
	}

	public void setPassword(String password) {
		this.password = password;
	}

	public String getTrustStore() {
		return trustStore;
	}

	public void setTrustStore(String trustStore) {
		this.trustStore = trustStore;
	}

	public String getTrustStorePassword() {
		return trustStorePassword;
	}

	public void setTrustStorePassword(String trustStorePassword) {
		this.trustStorePassword = trustStorePassword;
	}

	private long maxDelaySeconds;
	
	private String trustStore;
	
	private String trustStorePassword;
	
	public int getMaxRetries() {
		if(maxRetries != 0) {
			return maxRetries;
		}
		return Constants.MAXRETRIES;
	}

	public void setMaxRetries(int maxRetries) {
		this.maxRetries = maxRetries;
	}

	public long getMinDelaySeconds() {
		if(minDelaySeconds != 0) {
			return minDelaySeconds;
		}
		return Constants.MINDELAYSECONDS;
	}

	public void setMinDelaySeconds(long minDelaySeconds) {
		this.minDelaySeconds = minDelaySeconds;
	}

	public long getMaxDelaySeconds() {
		if(maxDelaySeconds != 0) {
			return maxDelaySeconds;
		}
		return Constants.MAXDELAYSECONDS;
	}

	public void setMaxDelaySeconds(long maxDelaySeconds) {
		this.maxDelaySeconds = maxDelaySeconds;
	}

	public long getBatchWaitSeconds() {
		if(batchWaitSeconds != 0) {
			return batchWaitSeconds;
		}
		return Constants.BATCHWAITSECONDS;
		
	}

	public void setBatchWaitSeconds(long batchWaitSeconds) {
		this.batchWaitSeconds = batchWaitSeconds;
	}

	public int getBatchSizeBytes() {
		if(batchSizeBytes != 0) {
			return batchSizeBytes;
		}
		return Constants.BATCHSIZEBYTES;
	}

	public void setBatchSizeBytes(int batchSizeBytes) {
		this.batchSizeBytes = batchSizeBytes;
	}

	public String getTenantID() {
		return tenantID;
	}

	public void setTenantID(String tenantID) {
		this.tenantID = tenantID;
	}

	public Map<String, String> getLabels() {
		return labels;
	}

	public void setLabels(Map<String, String> labels) {
		this.labels = labels;
	}

	public String getUrl() {
		if(url != "") {
			return url;
		}
		return Constants.URL; 
	}

	public void setUrl(String url) {
		this.url = url;
	}

	public URI getLokiURL() {
		return lokiURL;
	}

	public void setLokiURL(URI lokiURL) {
		this.lokiURL = lokiURL;
	}

	public void addLabels(Field field) {
		if (labels == null) {
			labels = new HashMap<String, String>();
		}
		labels.put(field.getKey(), field.getValue());
	}
	
	public static class Field {
		private String key;
		private String value;

		public String getKey() {
			return key;
		}

		public void setKey(String key) {
			this.key = key;
		}

		public String getValue() {
			return value;
		}

		public void setValue(String value) {
			this.value = "\"" + value + "\"";
		}

	}
}

