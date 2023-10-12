* Should be able to read bloom as a []byte without copying it during decoding
  * It's immutable + partition offsets are calculable, etc
  * can encode version, parameters as the last n bytes, each partition's byte range can be determined from that. No need to unpack
* implement streaming encoding.Decbuf over io.ReadSeeker
* Build & load from directories
* Less copying! I've taken some shortcuts we'll need to refactor to avoid copying []byte around in a few places
* more sophisticated querying methods
* queue access to blooms
* io.reader based decoder
* tar support