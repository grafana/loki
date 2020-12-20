# Squirrel - fluent SQL generator for Go

```go
import "gopkg.in/Masterminds/squirrel.v1"
```
or if you prefer using `master` (which may be arbitrarily ahead of or behind `v1`):

**NOTE:** as of Go 1.6, `go get` correctly clones the Github default branch (which is `v1` in this repo).
```go
import "github.com/Masterminds/squirrel"
```

[![GoDoc](https://godoc.org/github.com/Masterminds/squirrel?status.png)](https://godoc.org/github.com/Masterminds/squirrel)
[![Build Status](https://travis-ci.org/Masterminds/squirrel.svg?branch=v1)](https://travis-ci.org/Masterminds/squirrel)

_**Note:** This project has moved from `github.com/lann/squirrel` to
`github.com/Masterminds/squirrel`. Lann remains the architect of the
project, but we're helping him curate.

**Squirrel is not an ORM.** For an application of Squirrel, check out
[structable, a table-struct mapper](https://github.com/technosophos/structable)


Squirrel helps you build SQL queries from composable parts:

```go
import sq "github.com/Masterminds/squirrel"

users := sq.Select("*").From("users").Join("emails USING (email_id)")

active := users.Where(sq.Eq{"deleted_at": nil})

sql, args, err := active.ToSql()

sql == "SELECT * FROM users JOIN emails USING (email_id) WHERE deleted_at IS NULL"
```

```go
sql, args, err := sq.
    Insert("users").Columns("name", "age").
    Values("moe", 13).Values("larry", sq.Expr("? + 5", 12)).
    ToSql()

sql == "INSERT INTO users (name,age) VALUES (?,?),(?,? + 5)"
```

Squirrel can also execute queries directly:

```go
stooges := users.Where(sq.Eq{"username": []string{"moe", "larry", "curly", "shemp"}})
three_stooges := stooges.Limit(3)
rows, err := three_stooges.RunWith(db).Query()

// Behaves like:
rows, err := db.Query("SELECT * FROM users WHERE username IN (?,?,?,?) LIMIT 3",
                      "moe", "larry", "curly", "shemp")
```

Squirrel makes conditional query building a breeze:

```go
if len(q) > 0 {
    users = users.Where("name LIKE ?", fmt.Sprint("%", q, "%"))
}
```

Squirrel wants to make your life easier:

```go
// StmtCache caches Prepared Stmts for you
dbCache := sq.NewStmtCacher(db)

// StatementBuilder keeps your syntax neat
mydb := sq.StatementBuilder.RunWith(dbCache)
select_users := mydb.Select("*").From("users")
```

Squirrel loves PostgreSQL:

```go
psql := sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

// You use question marks for placeholders...
sql, _, _ := psql.Select("*").From("elephants").Where("name IN (?,?)", "Dumbo", "Verna")

/// ...squirrel replaces them using PlaceholderFormat.
sql == "SELECT * FROM elephants WHERE name IN ($1,$2)"


/// You can retrieve id ...
query := sq.Insert("nodes").
    Columns("uuid", "type", "data").
    Values(node.Uuid, node.Type, node.Data).
    Suffix("RETURNING \"id\"").
    RunWith(m.db).
    PlaceholderFormat(sq.Dollar)

query.QueryRow().Scan(&node.id)
```

You can escape question mask by inserting two question marks:

```sql
SELECT * FROM nodes WHERE meta->'format' ??| array[?,?]
```

will generate with the Dollar Placeholder:

```sql
SELECT * FROM nodes WHERE meta->'format' ?| array[$1,$2]
```



## License

Squirrel is released under the
[MIT License](http://www.opensource.org/licenses/MIT).
