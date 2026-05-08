# Compactor for Dataobjects

This is just an ideation doc intended to clarify priorities and open questions. A design doc will follow based on the discussion and priorities here.

## Key Responsibilities (Need prioritization) 

1. Deduplication  
2. Deletion requests  
3. Tenant specific retention  
4. Tenant Segregation (Single tenant objects)  
5. Compacting multiple smaller sized objects  
6. Schematizing data objects  
7. Reordering data within objects  
8. Reindexing  
9. Building parquet/iceberg to feed BizO11y

Vote based what should be the priority \-

### (➕ 1\) **Single tenant objects** *Technical Complexity: Medium Impact: High* 

- Both metastore and dataobjects are multi-tenant. The metastore must fetch all index files in that time range which in-turn fetch and read sections from multiple objects.   
- High volume streams cause multiple objects, that might also in-turn impact low volume tenants  
- Might improve query performance by improving locality  
- Makes tenant specific retention easier, no need to scan multiple objects  
- Makes custom deletion requests easier  
- Might result in non-uniform sized objects for some tenants.

Notes:

- Simple case \- don’t change / shuffle streams   
- Can rely on segmentation keys on ingestion path to start with

### (➕ 2\) **Deduplication** *Technical Complexity: Low Impact: Low* 

- Duplicate log lines are occasionally ingested due to client side retries  
- This can be addressed on the query path, however calls for reading all columns that affects read performance

Notes:

- Retries cause duplicates in case of partial success   
- More about correctness  
- Low impact as the probability of happening is low and we are prioritizing for performance

### (➕ 1\) **Deletion Requests** *Technical Complexity: Low Impact: Low* 

- Occasional requests from users to delete lines matching a stream selector/s  
- Can run on multiple data objects and clean up all sections that contain the streams

Notes:

- Probably blocker to production

(➕ 1\) **Tenant Specific retention** *Technical Complexity: Low Impact: Low* 

- Required for occasional housekeeping, adhering to compliance, etc. TCO?  
- Per stream retention??  
- This can also be addressed in the query path, but not ideal 

### (➕ 1\) **Compacting multiple smaller sized objects** *Technical Complexity: Medium Impact: Low* 

- Dataobjects are flushed when they reach ~~1G or 1h~~  512MB (compressed size) or the partition is idle for 5 mins  
- Delete requests and tenant specific retention in case of multi-tenant objects will also result in multiple non-uniform sized objects  
- Tenants with slower throughput might cause multiple objects with non-optimal smaller size, in turn resulting in multiple calls to the object store and thus performance

Notes:

- Probably can be merged to per-tenant objects and not require a separate topic  
- Can be considered as an extension of the per-tenant requirement. 

Points below can be considered for longer term  – 

### (➕ 0\) **Schematizing data objects** *Technical Complexity: Select Impact: Select* 

- We will be ingesting multiple tenants and all of their logs into the same dataobjects, as we start to parse log content into columns at ingestion this will make for very wide data objects which creates some real challenges, one of the main goals of compaction will be to try to align similar “schemas” of log lines within a section as much as possible to minimize the number of columns in a section.  
- This is a tricky problem, perhaps the first alignment we could do would be on the \`service\_name\` such that we fill sections with log lines from the same service, however, if you think about Loki, we have a service like “distributor” which has many distinctly different log patterns, one of our highest volume patterns is the “push request parsed” line, if we could separate this into its own sections it would have a very consistent schema and would be way more performant for querying.  
- We may want to leverage “drain” or another type of clustering algorithm to try to group log lines by similar patterns to minimize the number of columns within a section to reduce object metadata size and improve query performance.

### (➕ 0\) **Reordering data within objects** *Technical Complexity: Select Impact: Select* 

- Once data objects have been aligned around “schemas” better, how we sort the “primary key” column entirely depends on access patterns, for example with Loki we almost always query by namespace, so if we sort all of our rows by the namespace column this allows us to filter out large sections of data using metadata only as the metadata will show us a lexicographical “start” and “end” namespace for every section (or also pages within a section). This is a major way that columnar storage gets performance gains on top of selecting just a single column.  
- However, knowing what column to sort on is complex and is tied to the query/access patterns which means we won’t know this at ingestion time (at least not initially) so at compaction time we will want to be able to resort with new information we gain from querying patterns.  
- If we think about the hosted Grafana team access their logs, they never use namespace and instead only use “org” or “slug” so we would want to “schematize” their data into different sections/dataobject and then sort their data by “org”   
- We will likely want to consider the idea of re-writing or re-ordering old data on demand as well so that we can adapt to changing use patterns.  
- We will also likely want to consider “materialized views” where instead of replacing existing data we write a new copy and keep both, with different orderings for different access patterns.

### (➕ 0\) **Reindexing** *Technical Complexity: Select Impact: Select* 

- Our future Loki will likely support many index types for a column and is something that would likely change on demand to suit access patterns, one role of the compactor would be to scan a set of dataobjects and build a new index for them.  
- A lot is uncertain about this in a sense of where would these index files live, how would this work etc.

### (➕ 0\) **Building parquet/iceberg to feed BizO11y** *Technical Complexity: Select Impact: Select* 

- Loki will most likely eventually support more complex query patterns like joins and sub selects as well as SQL as a query language, but this won’t be for quite some time and the requests/demands to do more analytic heavy queries on log data will continue to exist.  
- One solution for this would be to have Loki generate an output format which is consumable by other tools like Trino or Athena such as creating parquet files with an Iceberg table/index definition.  
- This will also potentially play very nice with our new BizO11y initiative where this could consume these objects and we provide customers with analytics via this new department which could then have its own cost and access structure.  
- This model has Loki focus on handling recent data for observability use cases while taking really chaotic unstructured/semi-structured/structured log data and normalize it into properly structured parquet files. Dataobjects are still going to be important for these first stages of normalizing where the data will be widened and stored in objects with very complex dynamic schemas, however as we compact and normalize the data into more consistent schemas eventually we could output parquet for consumption but other tools.

### 

**Notes:**

- [Christian Haudum](mailto:christian.haudum@grafana.com)   
  - Rough priorities \- Deduplication, Single tenant objects and then Deletion requests.  
  - Can also explore apache Iceberg style snapshots to update and rebuild index files  
- [Sandeep Sukhani](mailto:sandeep.sukhani@grafana.com) \- will be resource intensive \- specifically scan the dataobject  
  - Can focus on avoiding compaction as much as possible  
  - Build single-tenant objects right from the start, compaction can run in the background for smaller sized objects  
    Could be config / or a rate based approach  
- Single tenant objects \-   
  - Problem when to flush an object to disk  
  - Multiple topics per tenant   
- [Christian Haudum](mailto:christian.haudum@grafana.com) Can keep the option open to build single tenant objects for bigger customers/ multitenant objects for smaller tenants. Needs to have dynamic number of tenants  
- [Ben Clive](mailto:ben.clive@grafana.com) Feasibility \- kafka needs static number of partitions per topic. Will be tricky   
- [Christian Haudum](mailto:christian.haudum@grafana.com) \- when we encode the new object, we can use the filesystem (scratch head) instead of reading everything in memory  
- [Sandeep Sukhani](mailto:sandeep.sukhani@grafana.com)single tenant objects make retention easier  
- We need to separate out the concerns of tenant retention and per-stream retention  
- What if we construct   
- [Ben Clive](mailto:ben.clive@grafana.com) \- single tenant objects for query performance   
- [Christian Haudum](mailto:christian.haudum@grafana.com) Deduplication can be done as a side-effect of compaction  
- [Sandeep Sukhani](mailto:sandeep.sukhani@grafana.com)T-shirt sized topics for customers  
- 