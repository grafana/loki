**How will deletions on dataobjects work?**

\[1\] Customer pushes in a delete request via the deletes API 

```go
DeleteRequest{
    RequestID: "req-123",
    TenantID: "tenant-foo",
    Query: `{job="test"}`,
    StartTime: t1,
    EndTime: t2,
    Status: "received"
  }
```

\[2\] Compaction main job (DeletionManager) identifies candidates for deletes stored in the deletes DB \- sqlite or boltdb and builds dataobj tombstones per delete request.

- Determine label matchers (line filters not supported in the initial version)  
- Query metastore to get matching sections  
- Group by object   
- Create dataobj-tombstone files (\*.tomb) stored under /\_\_tombstones\_\_/{tenantId}/req\_id.tomb  
- Update status to \`marked\` in DB for the delete requests

\[3\] Compaction main job (Sweeper \- similar to job builder) picks up tombstones and distributes to workers

- List tombstone marker files, for each object in the tombstone, create a job to rewrite –   
  - { objectPath: “...”., sectionsToDelete: \[1,2,3\], deleteRequestId: “” }  
- Distribute jobs to workers using grpc queues (existing mechanism using job builder  
-   and workers)

\[4\] Workers identify objects from tombstones and create new objects after applying deletes. Workers also append new sections under existing TOCs atomically after new objects are created. 

- Reads old object (object/ab/zyx-321), applies deletions by removing the relevant sections  
- Builds and uploads new object under a different hash \`objects/fe/abc-123\`  
- Atomically builds a new index object referencing the newly created object  
- Writes a version marker (see below)  
- Appends tombstone section to affect TOC windows. The IndexPointer section in TOC is still unchanged.

Tombstone sections in TOC:

```
  TOC Object at tocs/{timestamp}.toc:
  ├─ Header (THOR magic)
  ├─ Section 0: IndexPointers (unchanged)
  │  ├─ Column: path
  │  ├─ Column: min_timestamp
  │  └─ Column: max_timestamp
  ├─ Section 1: Tombstones (NEW)
  │  ├─ Column: obsolete_path 
  │  ├─ Column: replacement_path 
  │  ├─ Column: created_at
  │  └─ Column: reason // "deletion", "compaction"
  ├─ File Metadata
  └─ Footer

```

\[5\] Query path looks for tombstones within TOCs, if they exist, refer to the new objects.  
\[6\] Compaction clean up job then scans TOCs with existing tombstones, 

- updates IndexPointers to new paths referring to the tombstone sections  
- removes the tombstone section from TOCs  
- Deletes old object entirely

**Version Marker files:**

```protobuf
  message ObjectVersions {
    string original_path = 1;  // objects/abc123/def456
    repeated ObjectVersion versions = 2;
    string current_version_path = 3;  // objects/789xyz/uvw012
  }

  message ObjectVersion {
    string path = 1;
    int64 created_at = 2;
    int64 replaced_at = 3; // cleaned up
    string reason = 4;  // "deletion_sweep", "compaction"
  }
```

Why are they required? 

- During the cleanup loop, this manifest file can be referred to scan which TOC files still have tombstone sections  
- If persisted for N days, could provide a historical view and an audit trail of object modifications and aid rollback

Open questions:

1. How do we show deletion progress to the user when both dataobj and chunk exist simultaneously?  
2. Must address idempotency of requests \- multiple requests for the same matchers and time window

## Joe's writing things here

Deletion should be defined internally to the compactor as a programmatic interface. Something like:

```
ReplaceObjects(paths []string, replaceWith string) error
```

If delete object returns error the caller must handle this. e.g. if this is an http call 500 the response. If this is part of another job such as compaction or deletion then those processes need to persist state until this function succeeds.

### **Rules**

1. A given logs dataobject can only ever be referenced by a single index dataobject.  
2. A given index dataobject MAY be referenced by multiple TOC dataobjects.  
   1. The metastore is guaranteed to dedupe  
3. Through the TOC and index layers a given log line can only be discoverable once.  
4. After removal index and logs objects must remain in object storage for N minutes before being deleted to allow all thor components to recognize the change.

As we manipulate the data and logs objects we need to be careful that a given logline is always discoverable by the query scheduler and never duplicated. This will require an atomic change at some level.

### **Use cases** 	

1. Retention \- Block no longer has data within the tenant's time window.   
   1. paths is a slice with a single logs object to be removed  
   2. replaceWith is an empty string to indicate it's simply removed without replacement  
3. Logs Object Compaction \- Multiple logs objects were combined into one  
   1. paths is a slice with multiple logs objects that were combined  
   2. replaceWith contains the path of the new logs object that contains the compacted data.  
4. Index Object Compaction \- ???  
5. Logs Object rewriting (deletion or other mutation) \- One logs object is replaced by a new logs object  
   1. paths is a slice with a single string of the object to be removed.  
   2. replaceWith is a string indicating the new rewritten logs object.

### **Cleanup**

Due to unexpected exits/events we may occasionally orphan logs objects and indexes that are discoverable through the TOC layer. We can write a process to clean this up but it may be easier to simply let object storage retention policies clean them up.

### **Process**

This is partially a restatement of the above steps. Something not discussed is how to persist this job. If we fail somewhere in the deletion process we need to know that we were attempting it and start over. The above steps seem to talk about this with version markers.

**ReplaceObjects**

- Lock out all other logs object DeleteObjects requests  
- Identify the index object that reference all the logs objects in `paths`.   
  - Questions/Concerns:  
    How do we efficiently discover all indexes referencing a data object?  
- Rewrite index data objects to reference `replaceWith` and forget all data objects in `paths`  
- Add TOC tombstone entries as described above. At the moment we rewrite TOC we have swapped from old logs object to new object  
  - Questions/Concerns  
    How do we efficiently discover all TOCs that reference an index?   
    If an index is referenced by multiple TOCs then this is **NOT ATOMIC** b/c TOCs will be rewritten individually and the old and new logs objects will be simultaneously visible. How do we handle this?  
    We need to be careful about ballooning the TOC sizes b/c they are currently read in one ReadObject call.  
- Unlock other DeleteObject requests


**Cleanup**

- Discover tombstone in TOC that is greater than N minutes old.  
- Pull both old and new index objects. Discover any data objects that went from referenced \-\> unreferenced.  
- Remove all data objects that became unreferenced.  
- Remove the old index object.  
- Rewrite the TOC minus the tombstone.

