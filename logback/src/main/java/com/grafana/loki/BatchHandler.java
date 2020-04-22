package com.grafana.loki;

import java.io.IOException;
import java.net.URI;
import java.util.concurrent.BlockingQueue;

import javax.net.ssl.SSLContext;

import org.apache.http.client.methods.CloseableHttpResponse;
import org.apache.http.util.EntityUtils;

import com.grafana.loki.batch.Batch;
import com.grafana.loki.entry.Entry;
import com.grafana.loki.http.HttpHandler;
import com.grafana.loki.http.RetryHandler;

import ch.qos.logback.core.Context;
import ch.qos.logback.core.spi.ContextAwareBase;

public class BatchHandler extends ContextAwareBase implements Runnable {
	private volatile boolean running = true;

	private BlockingQueue<Entry> entryQueue;

	private BlockingQueue<Long> ticker;
	
	private LokiConfig config;

	private Batch batch;

	private String tenantID;

	private URI url;

	private String username;

	private String password;

	private long batchWait;

	private int batchSizeBytes;

	private SSLContext sslContext;

	private Context ctx;

	public BatchHandler(BlockingQueue<Entry> entryQueue, BlockingQueue<Long> ticker, LokiConfig config, Context ctx) {
		this.entryQueue = entryQueue;
		this.ticker = ticker;
		this.config = config;
		this.url = (URI) ctx.getObject("url");
		this.tenantID = config.getTenantID();
		this.username = config.getUsername();
		this.password = config.getPassword();
		this.batchWait = config.getBatchWaitSeconds() * 1000;
		this.batchSizeBytes = config.getBatchSizeBytes();
		this.sslContext = (SSLContext) ctx.getObject("sslContext");
		this.ctx = ctx;
		this.setContext(ctx);
	}

	@Override
	public void run() {
		try {
			while (running) {
				Entry e = entryQueue.poll();
				Long ticker = this.ticker.poll();

				if (e != null) {
					if (batch == null) {
						batch = new Batch(ctx);
						batch.add(e);
						continue;
					}

					String line = e.getLogprotoEntry().getLine();
					if (batch.sizeBytesAfter(line) > batchSizeBytes) {
						addInfo("Max batch_size is reached. Sending batch to loki");
						send(tenantID, batch);
						batch = new Batch(ctx);
						batch.add(e);
						continue;
					}
					batch.add(e);
				} else if (ticker != null) {
					if (batch != null) {
						if (batch.age() < batchWait) {
							continue;
						}
						addInfo("Max batch_wait time is reached. Sending batch to loki");
						send(tenantID, batch);
						batch = null;
					}
				}

			}
		} catch (Exception e) {
			addError("Exception in LokiBatchHandler", e);
		}

	}

	public void send(String tenantID, Batch batch) {
		byte[] snappyEncodedReq = batch.encode();
		RetryHandler retryHandler = new RetryHandler(ctx, config);
		HttpHandler httpHandler = new HttpHandler(ctx);

		String basicAuth = httpHandler.basicAuth(username, password);

		while (retryHandler.shouldRetry()) {
			try {
				CloseableHttpResponse response = httpHandler.sendLoki(tenantID, url, snappyEncodedReq, sslContext,
						basicAuth);
				if (response.getStatusLine().getStatusCode() > 0 && response.getStatusLine().getStatusCode() != 429
						&& response.getStatusLine().getStatusCode() / 100 != 5) {

					String body = "No Content";
					if (response.getEntity() != null) {
						body = EntityUtils.toString(response.getEntity());
					}

					addInfo("response from loki: body=" + body + ", status="
							+ response.getStatusLine().getStatusCode());

					break;
				}
				addWarn("Error sending batch");
				addWarn("Retrying request. Attempt number " + retryHandler.getRetries() + ". Retrying in "
						+ retryHandler.getDelay()/1000 + "s");
				retryHandler.retryRequest();
			} catch (IOException e) {
				if (e instanceof org.apache.http.conn.HttpHostConnectException) {
					addWarn("Error sending batch");
					addWarn("Retrying request. Attempt number " + retryHandler.getRetries() + ". Retrying in "
							+ retryHandler.getDelay()/1000 + "s");
					retryHandler.retryRequest();
					continue;
				}
				addError("Exception while sending logs to loki: ", e);
				break;
			}
		}
		
		try {
			httpHandler.closeConnections();
		}catch(IOException e) {
			addError("Exception while closing http connection: ",e);
		}
	}

	public void terminate() {
		running = false;
	}

	public Batch getBatch() {
		return batch;
	}

}
