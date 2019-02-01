// +build all cassandra

package gocql

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"gopkg.in/inf.v0"
)

type WikiPage struct {
	Title       string
	RevId       UUID
	Body        string
	Views       int64
	Protected   bool
	Modified    time.Time
	Rating      *inf.Dec
	Tags        []string
	Attachments map[string]WikiAttachment
}

type WikiAttachment []byte

var wikiTestData = []*WikiPage{
	{
		Title:    "Frontpage",
		RevId:    TimeUUID(),
		Body:     "Welcome to this wiki page!",
		Rating:   inf.NewDec(131, 3),
		Modified: time.Date(2013, time.August, 13, 9, 52, 3, 0, time.UTC),
		Tags:     []string{"start", "important", "test"},
		Attachments: map[string]WikiAttachment{
			"logo":    WikiAttachment("\x00company logo\x00"),
			"favicon": WikiAttachment("favicon.ico"),
		},
	},
	{
		Title:    "Foobar",
		RevId:    TimeUUID(),
		Body:     "foo::Foo f = new foo::Foo(foo::Foo::INIT);",
		Modified: time.Date(2013, time.August, 13, 9, 52, 3, 0, time.UTC),
	},
}

type WikiTest struct {
	session *Session
	tb      testing.TB

	table string
}

func CreateSchema(session *Session, tb testing.TB, table string) *WikiTest {
	table = "wiki_" + table
	if err := createTable(session, fmt.Sprintf("DROP TABLE IF EXISTS gocql_test.%s", table)); err != nil {
		tb.Fatal("CreateSchema:", err)
	}

	err := createTable(session, fmt.Sprintf(`CREATE TABLE gocql_test.%s (
			title       varchar,
			revid       timeuuid,
			body        varchar,
			views       bigint,
			protected   boolean,
			modified    timestamp,
			rating      decimal,
			tags        set<varchar>,
			attachments map<varchar, blob>,
			PRIMARY KEY (title, revid)
		)`, table))

	if err != nil {
		tb.Fatal("CreateSchema:", err)
	}

	return &WikiTest{
		session: session,
		tb:      tb,
		table:   table,
	}
}

func (w *WikiTest) CreatePages(n int) {
	var page WikiPage
	t0 := time.Now()
	for i := 0; i < n; i++ {
		page.Title = fmt.Sprintf("generated_%d", (i&16)+1)
		page.Modified = t0.Add(time.Duration(i-n) * time.Minute)
		page.RevId = UUIDFromTime(page.Modified)
		page.Body = fmt.Sprintf("text %d", i)
		if err := w.InsertPage(&page); err != nil {
			w.tb.Error("CreatePages:", err)
		}
	}
}

func (w *WikiTest) InsertPage(page *WikiPage) error {
	return w.session.Query(fmt.Sprintf(`INSERT INTO %s
		(title, revid, body, views, protected, modified, rating, tags, attachments)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, w.table),
		page.Title, page.RevId, page.Body, page.Views, page.Protected,
		page.Modified, page.Rating, page.Tags, page.Attachments).Exec()
}

func (w *WikiTest) SelectPage(page *WikiPage, title string, revid UUID) error {
	return w.session.Query(fmt.Sprintf(`SELECT title, revid, body, views, protected,
		modified,tags, attachments, rating
		FROM %s WHERE title = ? AND revid = ? LIMIT 1`, w.table),
		title, revid).Scan(&page.Title, &page.RevId,
		&page.Body, &page.Views, &page.Protected, &page.Modified, &page.Tags,
		&page.Attachments, &page.Rating)
}

func (w *WikiTest) GetPageCount() int {
	var count int
	if err := w.session.Query(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, w.table)).Scan(&count); err != nil {
		w.tb.Error("GetPageCount", err)
	}
	return count
}

func TestWikiCreateSchema(t *testing.T) {
	session := createSession(t)
	defer session.Close()

	CreateSchema(session, t, "create")
}

func BenchmarkWikiCreateSchema(b *testing.B) {
	b.StopTimer()
	session := createSession(b)
	defer func() {
		b.StopTimer()
		session.Close()
	}()

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		CreateSchema(session, b, "bench_create")
	}
}

func TestWikiCreatePages(t *testing.T) {
	session := createSession(t)
	defer session.Close()

	w := CreateSchema(session, t, "create_pages")

	numPages := 5
	w.CreatePages(numPages)
	if count := w.GetPageCount(); count != numPages {
		t.Errorf("expected %d pages, got %d pages.", numPages, count)
	}
}

func BenchmarkWikiCreatePages(b *testing.B) {
	b.StopTimer()
	session := createSession(b)
	defer func() {
		b.StopTimer()
		session.Close()
	}()

	w := CreateSchema(session, b, "bench_create_pages")

	b.StartTimer()

	w.CreatePages(b.N)
}

func BenchmarkWikiSelectAllPages(b *testing.B) {
	b.StopTimer()
	session := createSession(b)
	defer func() {
		b.StopTimer()
		session.Close()
	}()
	w := CreateSchema(session, b, "bench_select_all")

	w.CreatePages(100)
	b.StartTimer()

	var page WikiPage
	for i := 0; i < b.N; i++ {
		iter := session.Query(fmt.Sprintf(`SELECT title, revid, body, views, protected,
			modified, tags, attachments, rating
			FROM %s`, w.table)).Iter()
		for iter.Scan(&page.Title, &page.RevId, &page.Body, &page.Views,
			&page.Protected, &page.Modified, &page.Tags, &page.Attachments,
			&page.Rating) {
			// pass
		}
		if err := iter.Close(); err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkWikiSelectSinglePage(b *testing.B) {
	b.StopTimer()
	session := createSession(b)
	defer func() {
		b.StopTimer()
		session.Close()
	}()
	w := CreateSchema(session, b, "bench_select_single")
	pages := make([]WikiPage, 100)
	w.CreatePages(len(pages))
	iter := session.Query(fmt.Sprintf(`SELECT title, revid FROM %s`, w.table)).Iter()
	for i := 0; i < len(pages); i++ {
		if !iter.Scan(&pages[i].Title, &pages[i].RevId) {
			pages = pages[:i]
			break
		}
	}
	if err := iter.Close(); err != nil {
		b.Error(err)
	}
	b.StartTimer()

	var page WikiPage
	for i := 0; i < b.N; i++ {
		p := &pages[i%len(pages)]
		if err := w.SelectPage(&page, p.Title, p.RevId); err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkWikiSelectPageCount(b *testing.B) {
	b.StopTimer()
	session := createSession(b)
	defer func() {
		b.StopTimer()
		session.Close()
	}()

	w := CreateSchema(session, b, "bench_page_count")
	const numPages = 10
	w.CreatePages(numPages)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if count := w.GetPageCount(); count != numPages {
			b.Errorf("expected %d pages, got %d pages.", numPages, count)
		}
	}
}

func TestWikiTypicalCRUD(t *testing.T) {
	session := createSession(t)
	defer session.Close()

	w := CreateSchema(session, t, "crud")

	for _, page := range wikiTestData {
		if err := w.InsertPage(page); err != nil {
			t.Error("InsertPage:", err)
		}
	}
	if count := w.GetPageCount(); count != len(wikiTestData) {
		t.Errorf("count: expected %d, got %d\n", len(wikiTestData), count)
	}
	for _, original := range wikiTestData {
		page := new(WikiPage)
		if err := w.SelectPage(page, original.Title, original.RevId); err != nil {
			t.Error("SelectPage:", err)
			continue
		}
		sort.Sort(sort.StringSlice(page.Tags))
		sort.Sort(sort.StringSlice(original.Tags))
		if !reflect.DeepEqual(page, original) {
			t.Errorf("page: expected %#v, got %#v\n", original, page)
		}
	}
}
