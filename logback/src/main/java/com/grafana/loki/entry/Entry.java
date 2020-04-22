package com.grafana.loki.entry;

import java.util.Map;

public class Entry {
	private String tenantID;
	
	private Map<String, String> labels;
	
	private com.grafana.loki.logproto.Logproto.Entry logprotoEntry;

	public Entry(Map<String, String> labels, com.grafana.loki.logproto.Logproto.Entry entry) {
		this.labels = labels;
		this.logprotoEntry = entry;
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

	public com.grafana.loki.logproto.Logproto.Entry getLogprotoEntry() {
		return logprotoEntry;
	}

	public void setLogprotoEntry(com.grafana.loki.logproto.Logproto.Entry logprotoEntry) {
		this.logprotoEntry = logprotoEntry;
	}

}
