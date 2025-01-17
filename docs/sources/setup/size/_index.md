---
_build:
  list: false
noindex: true
title: Size the cluster 
menuTitle: Size the cluster 
description: Provides a tool that generates a Helm Chart values.yaml file based on expected ingestion, retention rate, and node type, to help size your Grafana deployment.
aliases:
  - ../installation/sizing/
  - ../installation/helm/generate
weight: 100
---

<link rel="stylesheet" href="../../query/analyzer/style.css">

# Size the cluster
<!-- vale Grafana.Quotes = NO -->
<!-- vale Grafana.Quotes = YES -->

This tool helps to generate a Helm Charts `values.yaml` file based on specified
 expected ingestion, retention rate and node type. It will always configure a
 [scalable]({{< relref "../../get-started/deployment-modes#simple-scalable" >}}) deployment. The storage needs to be configured after generation.

<div id="app">
    <label>Node Type<i class="fa fa-question" v-on:mouseover="help='node'" v-on:mouseleave="help=null"></i></label>
    <select name="node-type" v-model="node">
    <option v-for="node of nodes">{{ node }}</option>
    </select><br>
</div>

{{< tabs >}}
{{< tab-content name="~1PB/month (30TB/day)" >}}
| Component       | CPU Request | Memory Request | Base Replicas  |
|------------------|-------------|----------------|----------------|
| Ingester         | 4           | 8              | 150            |
| Distributor      | 2           | 1              | 100            |
| Index gateway    | 1           | 4              | 20             |
| Querier          | 1.5         | 3              | 250            |
| Query-frontend   | 1           | 4              | 16             |
| Query-scheduler  | 2           | 0.5            | 2              |
| Compactor        | 6           | 20             | 1 (Singleton)  |
{{< /tab-content >}}
{{< tab-content name="100TB to 1PB /month (3-30TB/day)" >}}
| Component       | CPU Request | Memory Request | Base Replicas  |
|------------------|-------------|----------------|----------------|
| Ingester         | 2           | 6              | 90             |
| Distributor      | 2           | 1              | 40             |
| Index gateway    | 0.5         | 4              | 10             |
| Querier          | 1.5         | 2              | 100            |
| Query-frontend   | 1           | 2              | 8              |
| Query-scheduler  | 1           | 0.5            | 2              |
| Compactor        | 6           | 20             | 1 (Singleton)  |
{{< /tab-content >}}
{{< tab-content name="Less than 100TB/month (3TB/day)" >}}
| Component       | CPU Request | Memory Request | Base Replicas  |
|------------------|-------------|----------------|----------------|
| Ingester         | 2           | 4              | 6              |
| Distributor      | 2           | 0.5            | 4              |
| Index gateway    | 0.5         | 2              | 4              |
| Querier          | 1           | 1              | 10             |
| Query-frontend   | 1           | 2              | 2              |
| Query-scheduler  | 1           | 0.5            | 2              |
| Compactor        | 2           | 10             | 1 (Singleton)  |
{{< /tab-content >}}
{{< /tabs >}}   


  <blockquote v-if="help">
    <span v-else-if="help === 'node'">
    Defines the node type of the Kubernetes cluster. Is a vendor or type
    missing? If so, add it to <code>pkg/sizing/node.go</code>.
    </span>
  </blockquote>


<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
<style>

#app label.icon.question::after {
  content: '\f29c';
  display: inline-block;
  font: normal normal normal 14px/1 FontAwesome;
  padding-left: 8px;
}

#app a {
  padding: .5em;

}
}
</style>

<script>
const API_URL = `https://logql-analyzer.grafana.net/next/api/sizing`
const { createApp } = Vue

createApp({
  data() {
    return {
      nodes: ["Loading..."],
      node: "Loading...",
      help: null,
    }
  },

  computed: {
    helmURL() {
      return `${API_URL}/helm?${this.queryString}`
    },
    queryString() {
      return `node-type=${encodeURIComponent(this.node)}&ingest=${encodeURIComponent(this.bytesDayIngest)}&retention=${encodeURIComponent(this.retention)}&queryperf=${encodeURIComponent(this.queryperf)}`
    },
    ingestInGB: {
	get () {
                if (this.bytesDayIngest == null) {
                    return null
                }
                // Convert to GB
                return this.bytesDayIngest / 1000 / 1000 / 1000
	},
	set (gbDayIngest) {
		console.log(gbDayIngest)
		this.bytesDayIngest = gbDayIngest * 1000 * 1000 * 1000
		console.log(this.bytesDayIngest)
	}
    }
  },

  created() {
    // fetch on init
    this.fetchNodeTypes()
  },

  methods: {
    async fetchNodeTypes() {
      const url = `${API_URL}/nodes`
      this.nodes = await (await fetch(url,{mode: 'cors'})).json()
    },
  },

  watch: {
    node:           'calculateClusterSize',
  }
}).mount('#app')
</script>
