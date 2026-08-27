package cluster

var (
	tsdbShipperSchemaConfigTemplate = `
schema_config:
  configs:
    - from: "{{.curPeriodStart}}"
      store: tsdb
      object_store: filesystem
      schema: {{.schemaVer}}
      index:
        prefix: index_
        period: 24h
`
	additionalTSDBShipperSchemaConfigTemplate = `
schema_config:
  configs:
    - from: "{{.additionalPeriodStart}}"
      store: tsdb
      object_store: store-1
      schema: {{.schemaVer}}
      index:
        prefix: index_tsdb_
        period: 24h
`
)

func SchemaWithTSDB(c *Cluster) {
	c.periodCfgs = append(c.periodCfgs, additionalTSDBShipperSchemaConfigTemplate)
}

func SchemaWithTSDBAndTSDB(c *Cluster) {
	c.periodCfgs = append(c.periodCfgs, additionalTSDBShipperSchemaConfigTemplate, tsdbShipperSchemaConfigTemplate)
}
