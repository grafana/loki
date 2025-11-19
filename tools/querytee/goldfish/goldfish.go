// Package goldfish provides query sampling and comparison functionality for the querytee tool.
// It enables A/B testing between two query backends (cells) by sampling queries, comparing
// their responses (including performance metrics and content hashes), and persisting results
// for analysis.
package goldfish
