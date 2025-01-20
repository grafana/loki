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

This section is a guide to size base resource needs of a Loki cluster.

Based on the expected ingestion volume, Loki clusters can be categorised into three tiers. Recommendations below are based on p90 resource utilisations of the relevant components. Each tab represents a different tier.
Please use this document as a rough guide to specify CPU and Memory requests in your deployment. This is only documented for microservices/distributed mode at this time.

Query resource needs can greatly vary with usage patterns and correct configurations. General notes on Query Performance:
- The rule of thumb is to run as small and many queriers as possible. Unoptimised queries can easily require 10x of the suggested querier resources below in all tiers. Running horizontal autoscaling will be most cost effective solution to meet the demand.
- Use this [blog post](https://grafana.com/blog/2023/12/28/the-concise-guide-to-loki-how-to-get-the-most-out-of-your-query-performance/) to adopt best practices for optimised query performance.
- Parallel-querier and related components can be sized the same along with queriers for starters, depending on how much Loki rules are used.
- Large Loki clusters benefits from disk based caching solution, memcached-extstore. Please see a detailed [blog post](https://grafana.com/blog/2023/08/23/how-we-scaled-grafana-cloud-logs-memcached-cluster-to-50tb-and-improved-reliability/) and read more on [memcached/nvm-caching here](https://memcached.org/blog/nvm-caching/).
- If you’re running a cluster that handles less than 30TB/day (~1PB/month) ingestion, we do not recommend configuring memcached-extstore. The additional operational complexity does not justify the savings.

These are the node types we suggest from various cloud providers. Please see the relevant specs on the provider documents.
<div id="app">
    <label>Node Type<i class="fa fa-question" v-on:mouseover="help='node'" v-on:mouseleave="help=null"></i></label>
    <select name="node-type" v-model="node">
    <option v-for="node of nodes">{{ node }}</option>
    </select><br>
</div>

{{< tabs >}}
{{< tab-content name="Less than 100TB/month (3TB/day)" >}}
| Component        | CPU Request | Memory Request (Gi)| Base Replicas | Total CPU Req |Total Mem Req (Gi)|
|------------------|-------------|-------------------|----------------|----------------|-----------------|
| Ingester         | 2           | 4                 | 6              | 12             | 36              |
| Distributor      | 2           | 0.5               | 4              | 8              | 2               |
| Index gateway    | 0.5         | 2                 | 4              | 2              | 8               |
| Querier          | 1           | 1                 | 10             | 10             | 10              |
| Query-frontend   | 1           | 2                 | 2              | 2              | 4               |
| Query-scheduler  | 1           | 0.5               | 2              | 2              | 1               |
| Compactor        | 2           | 10                | 1 (Singleton)  | 2              | 10              |
{{< /tab-content >}}
{{< tab-content name="100TB to 1PB /month (3-30TB/day)" >}}
| Component        | CPU Request | Memory Request (Gi)| Base Replicas | Total CPU Req |Total Mem Req (Gi)|
|------------------|-------------|-------------------|----------------|----------------|-----------------|
| Ingester         | 2           | 6                 | 90             | 180            | 540             |
| Distributor      | 2           | 1                 | 40             | 80             | 40              |
| Index gateway    | 0.5         | 4                 | 10             | 5              | 40              |
| Querier          | 1.5         | 2                 | 100            | 150            | 200             |
| Query-frontend   | 1           | 2                 | 8              | 8              | 16              |
| Query-scheduler  | 1           | 0.5               | 2              | 2              | 1               |
| Compactor        | 6           | 20                | 1 (Singleton)  | 6              | 20              |
{{< /tab-content >}}
{{< tab-content name="~1PB/month (30TB/day)" >}}
| Component        | CPU Request | Memory Request (Gi)| Base Replicas | Total CPU Req |Total Mem Req (Gi)|
|------------------|-------------|-------------------|----------------|----------------|-----------------|
| Ingester         | 4           | 8                 | 150            | 600            | 1200            |
| Distributor      | 2           | 1                 | 100            | 200            | 100             |
| Index gateway    | 1           | 4                 | 20             | 20             | 80              |
| Querier          | 1.5         | 3                 | 250            | 375            | 750             |
| Query-frontend   | 1           | 4                 | 16             | 16             | 64              |
| Query-scheduler  | 2           | 0.5               | 2              | 4              | 1               |
| Compactor        | 6           | 40                | 1 (Singleton)  | 6              | 40              |
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
