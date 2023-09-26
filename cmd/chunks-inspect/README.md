# Tools for inspecting Loki chunks

This tool can parse Loki chunks and print details from them. Useful for Loki developers.

To build the tool, simply run `go build` in this directory. Running resulting program with chunks file name gives you some basic chunks information:

```shell
$ ./chunks-inspect db61b4eca2a5ad68\:16f89ff4164\:16f8a0cfb41\:1538ace0 

Chunks file: db61b4eca2a5ad68:16f89ff4164:16f8a0cfb41:1538ace0
Metadata length: 485
Data length: 264737
UserID: 29
From: 2020-01-09 11:10:04.644000 UTC
Through: 2020-01-09 11:25:04.193000 UTC (14m59.549s)
Labels:
	 __name__ = logs
	 app = graphite
	 cluster = us-central1
	 color = b
	 container_name = graphite
	 filename = /var/log/pods/metrictank_graphite-1-large-multitenant-b-5f9db68b5c-jh769_ca9a10b0-0d2d-11ea-b85a-42010a80017a/graphite/0.log
	 hosted_metrics = 1
	 instance = graphite-1-large-multitenant-b-5f9db68b5c-jh769
	 job = metrictank/graphite
	 namespace = metrictank
	 org = 1
	 plan = large
	 pod_template_hash = 5f9db68b5c
	 stream = stderr
Encoding: lz4
Blocks Metadata Checksum: 3444d7a3 OK
Found 5 block(s), use -b to show block details
Minimum time (from first block): 2020-01-09 11:10:04.644490 UTC
Maximum time (from last block): 2020-01-09 11:25:04.192368 UTC
Total size of original data: 1257319 file size: 265226 ratio: 4.74
```

To print more details about individual blocks inside chunks, use `-b` parameter:

```shell script
$ ./chunks-inspect -b db61b4eca2a5ad68\:16f89ff4164\:16f8a0cfb41\:1538ace0

... chunk file info, see above ...
 
Block    0: position:        6, original length: 273604 (stored:  56220, ratio: 4.87), minT: 2020-01-09 11:10:04.644490 UTC maxT: 2020-01-09 11:12:53.458289 UTC, checksum: 13e73d71 OK
Block    0: digest compressed: ae657fdbb2b8be55eebe86b31a21050de2b5e568444507e5958218710ddf02fd, original: 0dad619bf3049a1152cb3153d90c6db6c3f54edbf9977753dde3c4e1b09d07b4
Block    1: position:    56230, original length: 274703 (stored:  60861, ratio: 4.51), minT: 2020-01-09 11:12:53.461855 UTC maxT: 2020-01-09 11:16:35.420787 UTC, checksum: 55269e65 OK
Block    1: digest compressed: a7999f471f68cce0458ff9790e7e7501c5bfe14cc28661d8670b9d88aeaee96f, original: a617a9e0b6c33aeaa83833470cf6164c540a7a64258e55eec6fdff483059df6f
Block    2: position:   117095, original length: 273592 (stored:  56563, ratio: 4.84), minT: 2020-01-09 11:16:35.423228 UTC maxT: 2020-01-09 11:19:28.680048 UTC, checksum: 781dba21 OK
Block    2: digest compressed: 65b59cc61c5eeea8116ce8a8c0b0d98b4d4671e8bc91656979c93717050a18fc, original: 896cc6487365ad0590097794a202aad5c89776d1c626f2cea33c652885939ac6
Block    3: position:   173662, original length: 273745 (stored:  57486, ratio: 4.76), minT: 2020-01-09 11:19:31.062836 UTC maxT: 2020-01-09 11:23:13.562630 UTC, checksum: 2a88a52b OK
Block    3: digest compressed: 4f51a64d0397cc806a898cd6662695620083466f234d179fef5c2d02c9766191, original: 15e8a1833ccbba9aa8374029141a054127526382423d3a63f321698ff8e087b5
Block    4: position:   231152, original length: 161675 (stored:  33440, ratio: 4.83), minT: 2020-01-09 11:23:15.416284 UTC maxT: 2020-01-09 11:25:04.192368 UTC, checksum: 6d952296 OK
Block    4: digest compressed: 8dd12235f1d619c30a9afb66823a6c827613257773669fda6fbfe014ed623cd1, original: 1f7e8ef8eb937c87ad3ed3e24c321c40d43534cc43662f83ab493fb3391548b2
Total size of original data: 1257319 file size: 265226 ratio: 4.74
```

To also print individual log lines, use `-l` parameter. Full help:

```shell script
$ ./chunks-inspect -h
Usage of ./chunks-inspect:
  -b	print block details
  -l	print log lines
  -s	store blocks, using input filename, and appending block index to it
```

Parameter `-s` allows you to inspect individual blocks, both in compressed format (as stored in chunk file), and original raw format.
