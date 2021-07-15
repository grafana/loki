package com.grafana.loki.batch;

import java.io.IOException;
import java.util.HashMap;
import java.util.Map;

import org.xerial.snappy.Snappy;

import com.grafana.loki.entry.Entry;
import com.grafana.loki.logproto.Logproto;
import com.grafana.loki.logproto.Logproto.PushRequest;
import com.grafana.loki.logproto.Logproto.Stream;
import com.grafana.loki.logproto.Logproto.Stream.Builder;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.spi.ContextAwareBase;

public class Batch extends ContextAwareBase {
	private Map<String, Stream> streams;

	private int bytes;

	private long createdAt;

	public Batch(Context ctx) {
		this.streams = new HashMap<String, Stream>();
		this.bytes = 0;
		this.createdAt = System.nanoTime();
		this.setContext(ctx);
	}

	public void add(Entry entry) {
		this.bytes = this.bytes + entry.getLogprotoEntry().getLine().length();

		String labels = entry.getLabels().toString();
		if (streams.containsKey(labels)) {
			try {
				Stream strm = streams.get(labels);
				Builder streamBuilder = strm.toBuilder();
				streamBuilder.addEntries(entry.getLogprotoEntry());
				streams.put(labels, streamBuilder.build());
			} catch (Exception e) {
				addError("error adding entries to stream", e);
			}
		} else {
			Builder streamBuilder = com.grafana.loki.logproto.Logproto.Stream.newBuilder();
			streamBuilder.setLabels(labels);
			try {
				streamBuilder.addEntries(entry.getLogprotoEntry());
				streams.put(labels, streamBuilder.build());
			} catch (Exception e) {
				addError("error adding entries to stream", e);
			}
		}

	}

	public long age() {
		return System.nanoTime() - this.createdAt;
	}

	public int sizeBytes() {
		return this.bytes;
	}

	public int sizeBytesAfter(String line) {
		return this.bytes + line.length();
	}

	public byte[] encode() {
		PushRequest request = createPushRequest();
		byte[] snappyInput = null;
		try {
			snappyInput = Snappy.compress(request.toByteArray());
		} catch (IOException e) {
			addError("error encoding request", e);
		}
		return snappyInput;

	}

	public Logproto.PushRequest createPushRequest() {
		com.grafana.loki.logproto.Logproto.PushRequest.Builder pushRequest = PushRequest.newBuilder();
		for (Map.Entry<String, Stream> stream : streams.entrySet()) {
			pushRequest.addStreams(stream.getValue());
		}
		return pushRequest.build();
	}

	public Map<String, Stream> getStreams() {
		return streams;
	}

}
