* Should be able to read bloom as a []byte without copying it during decoding
  * It's immutable + partition offsets are calculable, etc
* Less copying! I've taken some shortcuts we'll need to refactor to avoid copying []byte around in a few places
* more sophisticated querying methods
* queue access to blooms