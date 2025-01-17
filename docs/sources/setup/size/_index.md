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
    </select>
    <div class="controls">
        <label class="radio-label">
            <input type="radio" name="tableSelector" value="table1" checked> Table 1
        </label>
        <label class="radio-label">
            <input type="radio" name="tableSelector" value="table2"> Table 2
        </label>
        <label class="radio-label">
            <input type="radio" name="tableSelector" value="table3"> Table 3
        </label>
    </div>
    <div id="table1" class="table-container">
        <table>
            <tr>
                <th>Component</th>
                <th>CPU Requet</th>
                <th>Memory Request</th>
                <th>Base Replicas</th>
            </tr>
            <tr>
                <td>Ingester</td>
                <td>4</td>
                <td>8</td>
                <td>150</td>
            </tr>
            <tr>
                <td>Distributor</td>
                <td>2</td>
                <td>1</td>
                <td>100</td>
            </tr>
            <tr>
                <td>Index gateway</td>
                <td>1</td>
                <td>4</td>
                <td>20</td>
            </tr>
            <tr>
                <td>Querier</td>
                <td>1.5</td>
                <td>3</td>
                <td>250</td>
            </tr>
            <tr>
                <td>Query-frontend</td>
                <td>1</td>
                <td>4</td>
                <td>16</td>
            </tr>
            <tr>
                <td>Query-scheduler</td>
                <td>2</td>
                <td>0.5</td>
                <td>2</td>
            </tr>
            <tr>
                <td>Compactor</td>
                <td>6</td>
                <td>20</td>
                <td>1 (Singleton)</td>
            </tr>
        </table>
    <div id="table2" class="table-container">
        <table>
            <tr>
                <th>Component</th>
                <th>CPU Requet</th>
                <th>Memory Request</th>
                <th>Base Replicas</th>
            </tr>
            <tr>
                <td>Ingester</td>
                <td>2</td>
                <td>6</td>
                <td>90</td>
            </tr>
            <tr>
                <td>Distributor</td>
                <td>2</td>
                <td>1</td>
                <td>40</td>
            </tr>
            <tr>
                <td>Index gateway</td>
                <td>0.5</td>
                <td>4</td>
                <td>10</td>
            </tr>
            <tr>
                <td>Querier</td>
                <td>1.5</td>
                <td>2</td>
                <td>100</td>
            </tr>
            <tr>
                <td>Query-frontend</td>
                <td>1</td>
                <td>2</td>
                <td>8</td>
            </tr>
            <tr>
                <td>Query-scheduler</td>
                <td>1</td>
                <td>0.5</td>
                <td>2</td>
            </tr>
            <tr>
                <td>Compactor</td>
                <td>6</td>
                <td>20</td>
                <td>1 (Singleton)</td>
            </tr>
        </table>
    <div id="table3" class="table-container">
        <table>
            <tr>
                <th>Component</th>
                <th>CPU Requet</th>
                <th>Memory Request</th>
                <th>Base Replicas</th>
            </tr>
            <tr>
                <td>Ingester</td>
                <td>2</td>
                <td>4</td>
                <td>6</td>
            </tr>
            <tr>
                <td>Distributor</td>
                <td>2</td>
                <td>0.5</td>
                <td>4</td>
            </tr>
            <tr>
                <td>Index gateway</td>
                <td>0.5</td>
                <td>2</td>
                <td>4</td>
            </tr>
            <tr>
                <td>Querier</td>
                <td>1</td>
                <td>1</td>
                <td>10</td>
            </tr>
            <tr>
                <td>Query-frontend</td>
                <td>1</td>
                <td>2</td>
                <td>2</td>
            </tr>
            <tr>
                <td>Query-scheduler</td>
                <td>1</td>
                <td>0.5</td>
                <td>2</td>
            </tr>
            <tr>
                <td>Compactor</td>
                <td>2</td>
                <td>10</td>
                <td>1 (Singleton)</td>
            </tr>
        </table>
    </div>

  <div v-if="clusterSize">
    <table>
    <tr>
      <th>Read Replicas</th>
      <th>Write Replicas</th>
      <th>Nodes</th>
      <th>Cores</th>
      <th>Memory</th>
    </tr>
    <tr>
      <td>{{ clusterSize.TotalReadReplicas }}</td>
      <td>{{ clusterSize.TotalWriteReplicas }}</td>
      <td>{{ clusterSize.TotalNodes}}</td>
      <td>{{ clusterSize.TotalCoresRequest}}</td>
      <td>{{ clusterSize.TotalMemoryRequest}} GB</td>
    </tr>
    </table>
  </div>

  <a v-bind:href="helmURL" class="primary-button">Generate and download values file</a>

  <blockquote v-if="help">
    <span v-if="help === 'ingest'">
    Defines the log volume in gigabytes, ie 1e+9 bytes, expected to be ingested each day.
    </span>
    <span v-else-if="help === 'node'">
    Defines the node type of the Kubernetes cluster. Is a vendor or type
    missing? If so, add it to <code>pkg/sizing/node.go</code>.
    </span>
    <span v-else-if="help === 'retention'">
    Defines how long the ingested logs should be kept.
    </span>
    <span v-else-if="help === 'queryperf'">
    Defines the expected query performance. Basic is sized for a max query throughput of around 3GB/s. Super aims for 25% more throughput.
    </span>
  </blockquote>
</div>

<script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
<style>

#app label.icon.question::after {
  content: '\f29c';
  display: inline-block;
  font: normal normal normal 14px/1 FontAwesome;
  padding-left: 8px;
}

#app #queryperf label {
  padding: 1em;
  text-align: center;
}

#app #queryperf label input {
  display: block;
}

#app a {
  padding: .5em;

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
      bytesDayIngest: null,
      retention: null,
      queryperf: 'Basic',
      help: null,
      clusterSize: null
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
    async calculateClusterSize() {
      if (this.node == 'Loading...' || this.bytesDayIngest== null || this.retention == null) {
        return
      }
      const url = `${API_URL}/cluster?${this.queryString}`
      this.clusterSize = await (await fetch(url,{mode: 'cors'})).json()
    }
  },

  watch: {
    node:           'calculateClusterSize',
    bytesDayIngest: 'calculateClusterSize',
    retention:      'calculateClusterSize',
    queryperf:      'calculateClusterSize'
  }
}).mount('#app')
</script>
