* Should be able to read bloom as a []byte without copying it during decoding
  * It's immutable + partition offsets are calculable, etc
* Less copying! I've taken some shortcuts we'll need to refactor to avoid copying []byte around in a few places
* more sophisticated querying methods
* queue access to blooms
* multiplex reads across blooms
* Queueing system for bloom access

# merge querier for different blocks
* how to merge two block queriers with the same fp
*  merge block querier should use iterator interface
  * As long as MergeBlockQuerier can Peek, we can make another querier impl to dedupe with some function (i.e. prefer series with more chunks indexed)