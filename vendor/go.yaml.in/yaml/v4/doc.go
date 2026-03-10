//
// Copyright (c) 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0
//

// Package yaml implements YAML 1.1/1.2 encoding and decoding for Go programs.
//
// # Quick Start
//
// For simple encoding and decoding, use [Unmarshal] and [Marshal]:
//
//	type Config struct {
//	    Name    string `yaml:"name"`
//	    Version string `yaml:"version"`
//	}
//
//	// Decode YAML to Go struct
//	var config Config
//	err := yaml.Unmarshal(yamlData, &config)
//
//	// Encode Go struct to YAML
//	data, err := yaml.Marshal(&config)
//
// For encoding/decoding with options, use [Load] and [Dump]:
//
//	// Decode with strict field checking
//	err := yaml.Load(data, &config, yaml.WithKnownFields())
//
//	// Encode with custom indent
//	data, err := yaml.Dump(&config, yaml.WithIndent(2))
//
//	// Decode all documents from multi-document stream
//	var docs []Config
//	err := yaml.Load(multiDocYAML, &docs, yaml.WithAllDocuments())
//
//	// Encode multiple documents as multi-document stream
//	docs := []Config{config1, config2}
//	data, err := yaml.Dump(docs, yaml.WithAllDocuments())
//
// # Streaming with Loader and Dumper
//
// For multi-document streams or when you need custom options, use [Loader] and [Dumper]:
//
//	// Load multiple documents from a stream
//	loader, err := yaml.NewLoader(reader)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for {
//	    var doc any
//	    if err := loader.Load(&doc); err == io.EOF {
//	        break
//	    } else if err != nil {
//	        log.Fatal(err)
//	    }
//	    // Process document...
//	}
//
//	// Dump multiple documents to a stream
//	dumper, err := yaml.NewDumper(writer, yaml.WithIndent(2))
//	if err != nil {
//	    log.Fatal(err)
//	}
//	dumper.Dump(&doc1)
//	dumper.Dump(&doc2)
//	dumper.Close()
//
// # Options System
//
// Configure YAML processing behavior with functional options:
//
//	yaml.NewDumper(w,
//	    yaml.WithIndent(2),              // Indentation spacing
//	    yaml.WithCompactSeqIndent(),     // Compact sequences (defaults to true)
//	    yaml.WithLineWidth(80),          // Line wrapping width
//	    yaml.WithUnicode(false),         // Escape non-ASCII (override default true)
//	    yaml.WithKnownFields(),          // Strict field checking (defaults to true)
//	    yaml.WithUniqueKeys(),           // Prevent duplicate keys (defaults to true)
//	    yaml.WithSingleDocument(),       // Single document mode
//	)
//
// Or use version-specific option presets for consistent formatting:
//
//	yaml.NewDumper(w, yaml.V3)
//
// Options can be combined and later options override earlier ones:
//
//	// Start with v3 defaults, then override indent
//	yaml.NewDumper(w,
//	    yaml.V3,
//	    yaml.WithIndent(4),
//	)
//
// Load options from YAML configuration files:
//
//	opts, err := yaml.OptsYAML(configYAML)
//	dumper, err := yaml.NewDumper(w, opts)
//
// # YAML Compatibility
//
// This package supports most of YAML 1.2, but preserves some YAML 1.1
// behavior for backward compatibility:
//
//   - YAML 1.1 booleans (yes/no, on/off) are supported when decoding into
//     typed bool values, otherwise treated as strings
//   - Octals can use 0777 format (YAML 1.1) or 0o777 format (YAML 1.2)
//   - Base-60 floats are not supported (removed in YAML 1.2)
//
// # Version Defaults
//
// [NewLoader] and [NewDumper] use v4 defaults (2-space indentation, compact
// sequences). The older [Marshal] and [Unmarshal] functions use v3 defaults
// for backward compatibility. Use the options system to select different
// version defaults if needed.
package yaml
