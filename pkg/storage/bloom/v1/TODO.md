* Need to implement a lazy decoder similar to encoding.Decbuf but backed by a io.ReadSeeker
  * Will allow seeking to offsets within a file and not buffering the entire file in memory
  * This is important for size-sensitive workloads such as many bloom block files, etc