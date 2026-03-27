// Package goldfish provides query sampling and comparison functionality for the querytee and Goldfish UI.
// It enables A/B testing between two query backends (cells) by sampling queries, comparing
// their responses (including performance metrics and content hashes), and persisting results
// for analysis. This package provides the data structures and SQL queries to persist and retrieve the results
// of this analysis.
package goldfish
