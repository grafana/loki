## Unreleased

### Improvements

- Add `Config.CompressionAlgorithm` to optionally select snappy compression as
  an alternative to the LZW default. Receivers always decode every supported
  algorithm; senders emit only the configured one. Default behaviour is
  unchanged.

  Rollout note: every cluster member must be upgraded to a build that decodes
  snappy BEFORE any member is configured to emit it. A receiver that does not
  know the algorithm logs `cannot decompress unknown algorithm` and drops the
  packet — it does not panic.
- Reduce per-call allocations on the compression and push-pull receive
  paths by reusing internal scratch buffers. Pools are internal only;
  public-surface returns allocate fresh memory each call. Covers:
  - LZW writers/readers and snappy destination buffers (compression).
  - A large-buffer pool for the push-pull receive path
    (`decryptRemoteState`), sized for `maxPushStateBytes`. Pooling
    here amortizes `io.CopyN` growth across receives. The send side
    (`encryptLocalState`) does not pool — `encryptPayload` pre-`Grow`s
    the destination to its exact encrypted length in one shot, so a
    fresh per-call buffer allocates once and never grows.
- Add per-algorithm compression metrics:
  `memberlist_compress_attempts_total{algo}`,
  `memberlist_compress_skipped_total{algo,reason="size_worse_than_original"}`,
  `memberlist_compress_errors_total{algo}`,
  `memberlist_decompress_attempts_total{algo}`,
  `memberlist_decompress_errors_total{algo}`.

### Changes

- TCP push-pull and other TCP stream messages now skip compression when
  the compressed output is no smaller than the input, falling back to a
  plaintext frame. Mirrors the existing UDP packet behaviour in
  `rawSendMsgPacket`. Receivers decode both compressed and plaintext
  frames, so the change is wire-compatible. Two operator-visible
  effects: incompressible TCP payloads (already-compressed, encrypted,
  or random bytes) are no longer wrapped in a `compressMsg` frame, and
  `memberlist_compress_skipped_total{algo,reason="size_worse_than_original"}`
  now fires on the TCP path as well as UDP.

### Fixed

### Security
