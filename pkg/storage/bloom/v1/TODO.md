* Less copying! I've taken some shortcuts we'll need to refactor to avoid copying []byte around in a few places
* more sophisticated querying methods
* queue access to blooms
* multiplex reads across blooms
* Queueing system for bloom access
* bloom hierarchies (bloom per block, etc). Test a tree of blooms down the to individual series/chunk
* memoize hashing & bucket lookups during queries
* encode bloom parameters in block


# merge querier for different blocks
* how to merge two block queriers with the same fp
*  merge block querier should use iterator interface
  * As long as MergeBlockQuerier can Peek, we can make another querier impl to dedupe with some function (i.e. prefer series with more chunks indexed)