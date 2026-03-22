// Copyright 2023 Roxy Light
// SPDX-License-Identifier: ISC

/*
Package sqlitex provides utilities for working with SQLite.

# Statements

To execute a statement from a string,
pass an [ExecOptions] struct to one of the following functions:

  - [Execute]
  - [ExecuteScript]
  - [ExecuteTransient]

To execute a statement from a file (typically using [embed]),
pass an [ExecOptions] struct to one of the following functions:

  - [ExecuteFS]
  - [ExecuteScriptFS]
  - [ExecuteTransientFS]
  - [PrepareTransientFS]

# Transactions and Savepoints

  - [Save]
  - [Transaction]
  - [ExclusiveTransaction]
  - [ImmediateTransaction]
*/
package sqlitex
