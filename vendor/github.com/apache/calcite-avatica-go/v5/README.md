<!--
{% comment %}
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to you under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
{% endcomment %}
-->

# Apache Avatica/Phoenix SQL Driver

[![GoDoc](https://godoc.org/github.com/apache/calcite-avatica-go?status.png)](https://godoc.org/github.com/apache/calcite-avatica-go)
[![Build Status](https://github.com/apache/calcite-avatica-go/workflows/Tests/badge.svg)](https://github.com/apache/calcite-avatica-go)

Apache Calcite's Avatica Go is a Go [database/sql](https://golang.org/pkg/database/sql/) driver for the Avatica server.

Avatica is a sub-project of [Apache Calcite](https://calcite.apache.org).

## Quick Start
Install using Go modules:

```
$ go get github.com/apache/calcite-avatica-go
```

The Phoenix/Avatica driver implements Go's `database/sql/driver` interface, so, import the
`database/sql` package and the driver:

```
import "database/sql"
import _ "github.com/apache/calcite-avatica-go/v5"

db, err := sql.Open("avatica", "http://localhost:8765")
```

Then simply use the database connection to query some data, for example:

```
rows := db.Query("SELECT COUNT(*) FROM test")
```

For more details, see the [home page](https://calcite.apache.org/avatica/docs/go_client_reference.html).

Release notes for all published versions are available on the [history
page](https://calcite.apache.org/avatica/docs/go_history.html).

## Issues
We do not use Github to file issues. Please create an issue on [Calcite's JIRA](https://issues.apache.org/jira/projects/CALCITE/issues)
and select `avatica-go` as the component.