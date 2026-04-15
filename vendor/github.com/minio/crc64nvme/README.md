
## crc64nvme

This Golang package calculates CRC64 checksums using carryless-multiplication accelerated with SIMD instructions for both ARM and x86. It is based on the NVME polynomial as specified in the [NVM Express® NVM Command Set Specification](https://nvmexpress.org/wp-content/uploads/NVM-Express-NVM-Command-Set-Specification-1.0d-2023.12.28-Ratified.pdf).

The code is based on the [crc64fast-nvme](https://github.com/awesomized/crc64fast-nvme.git) package in Rust and is released under the Apache 2.0 license.

For more background on the exact technique used, see this [Fast CRC Computation for Generic Polynomials Using PCLMULQDQ Instruction](https://web.archive.org/web/20131224125630/https://www.intel.com/content/dam/www/public/us/en/documents/white-papers/fast-crc-computation-generic-polynomials-pclmulqdq-paper.pdf) paper.

### Performance

To follow.

### Requirements

All Go versions >= 1.22 are supported.

### Contributing

Contributions are welcome, please send PRs for any enhancements.
