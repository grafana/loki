package com.grafana.loki;

public class Constants {
	
	public static final String URL = "http://localhost:3100/loki/api/v1/push";
	
	public static final int MAXRETRIES = 10;
	
	public static final int BATCHSIZEBYTES = 102400;
	
	public static final long BATCHWAITSECONDS = 1;
	
	public static final long MINDELAYSECONDS  = 1;
	
	public static final long MAXDELAYSECONDS = 300;

}
