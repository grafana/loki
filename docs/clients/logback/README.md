# Logback

Loki has a [logback](http://logback.qos.ch/) appender plugin that enables shipping logs to a private Loki
instance or [Grafana Cloud](https://grafana.com/oss/loki).

## Installation

Compile and Install to maven repository

```
$ mvn compile install
```

Include below configuration in your projects pom.xml

``` 
<dependency>
    <groupId>ch.qos.logback</groupId>
    <artifactId>logback-classic</artifactId>
    <version>${logback.version}</version>
</dependency>
<dependency>
    <groupId>com.grafana.loki</groupId>
    <artifactId>logback-appender</artifactId>
    <version>$version</version>
</dependency>
```

## Usage

In your Logback configuration, use `com.grafana.loki.LokiAppender`. The configuration values would look like this.

```
<?xml version="1.0" encoding="UTF-8"?>
<configuration>
   <appender name="loki" class="com.grafana.loki.LokiAppender">
    <encoder>
      <Pattern>%d{YYY-dd-MM HH:mm:ss.SSS} [%thread] %-5level %logger{36} - %msg%n</Pattern>
    </encoder>
    <lokiConfig>
    	[<url>String</url> | default = null | required = True]
    	[<tenantID>String</tenantID> | default = null | required = False]
    	[<batchWaitSeconds>long</batchWaitSeconds> | default = 1 | required = False]
    	[<batchSizeBytes>int</batchSizeBytes> | default = 102400 | required = False]
    	[<username>String</username> | default = null | required = False]
    	[<password>String</password> | default = null | required = False]
    	[<trustStore>String</trustStore | default = null | required = False>
    	[<trustStorePassword>String</trustStorePassword | default = null | required = False>
    	[<minDelaySeconds>long</minDelaySeconds | default = 1 | required = False>
    	[<maxDelaySeconds>long</maxDelaySeconds | default = 300 | required = False>
    	[<maxRetries>int</maxRetries | default = 10 | required = False>
    	[<labels>
    		<key>test</key>
    		<value>value</value>
    	</labels> | default = null | required = False]
    </lokiConfig>
  </appender>

  <root level="debug">
    <appender-ref ref="loki"/>
  </root>
</configuration>
```

## Configuration

### url

The url of the Loki server to send logs to.
When sending data the push path need to also be provided e.g. `http://localhost:3100/loki/api/v1/push`.

### tenantID

Loki is a multi-tenant log storage platform and all requests sent must include a tenant.  For some installations the tenant will be set automatically by an authenticating proxy.  Otherwise you can define a tenant to be passed through.  The tenant can be any string value.

### labels

All logs send to Loki will be added with these extra labels.

### batchWaitSeconds

Interval in seconds to wait before pushing a batch of records to loki

### batchSizeBytes

Maximum batch size to accrue before pushing to loki. Defaults to 102400 bytes

### Backoff config

#### minDelaySeconds

Initial backoff time between retries

#### maxDelaySeconds => 300(5m)

Maximum backoff time between retries

#### maxRetries => 10

Maximum number of retries to do

### username / password

Specify a username and password if the Loki server requires basic authentication.
If using the GrafanaLab's hosted Loki, the username needs to be set to your instanceId and the password should be a Grafana.com api key.

### trustStore
Specify the location of a trust store containing root CA certificates used for validating server trust

### trustStorePassword
Specify the password for trust store containing root CA certificates used for validating server trust

## Example configuration with default values

```
<?xml version="1.0" encoding="UTF-8"?>
<configuration debug="true">
   <appender name="loki" class="com.grafana.loki.LokiAppender">
    <encoder>
      <Pattern>%d{YYY-dd-MM HH:mm:ss.SSS} [%thread] %-5level %logger{36} - %msg%n</Pattern>
    </encoder>
    <lokiConfig>
    	<url>http://localhost:3100/loki/api/v1/push</url>
    	<tenantID>fake</tenantID>
    	<batchWaitSeconds>1</batchWaitSeconds>
    	<batchSizeBytes>102400</batchSizeBytes>
    	<username></username>
    	<password></password>
    	<trustStore></trustStore>
    	<trustStorePassword></trustStorePassword>
    	<minDelaySeconds>1</minDelaySeconds>
    	<maxDelaySeconds>300</maxDelaySeconds>
    	<maxRetries>10</maxRetries>
    	<labels>
    		<key></key>
    		<value></value>
    	</labels>
    </lokiConfig>
  </appender>

  <root level="debug">
    <appender-ref ref="loki"/>
  </root>
</configuration>
```

## Example configuration with custom values

```
<?xml version="1.0" encoding="UTF-8"?>
<configuration debug="true">
   <appender name="loki" class="com.grafana.loki.LokiAppender">
    <encoder>
      <Pattern>%d{YYY-dd-MM HH:mm:ss.SSS} [%thread] %-5level %logger{36} - %msg%n</Pattern>
    </encoder>
    <lokiConfig>
    	<url>http://localhost:3100/loki/api/v1/push</url>
    	<tenantID>fake</tenantID>
    	<batchWaitSeconds>1</batchWaitSeconds>
    	<batchSizeBytes>102400</batchSizeBytes>
    	<username></username>
    	<password></password>
    	<trustStore>/path/to/truststore</trustStore>
    	<trustStorePassword>****</trustStorePassword>
    	<minDelaySeconds>1</minDelaySeconds>
    	<maxDelaySeconds>60</maxDelaySeconds>
    	<maxRetries>3</maxRetries>
    	<labels>
    		<key>test</key>
    		<value>value</value>
    	</labels>
        <labels>
    		<key>test2</key>
    		<value>value2</value>
    	</labels>
    </lokiConfig>
  </appender>

  <root level="debug">
    <appender-ref ref="loki"/>
  </root>
</configuration>
```
