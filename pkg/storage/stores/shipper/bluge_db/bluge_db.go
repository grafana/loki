package bluge_db

import (
	"context"
	"fmt"
	"github.com/blugelabs/bluge"
	//"github.com/cortexproject/cortex/pkg/chunk/local"

	//"github.com/cortexproject/cortex/pkg/chunk/local"
	"log"
)

type BlugeDB struct {
	Name   string
	Folder string // snpseg
}

func NewDB(name string, path string) *BlugeDB {
	return &BlugeDB{Name: name, Folder: path} // "./snpseg"
}

type logItem []logLabel
type logLabel struct {
	name  string
	value string
}

//func (b *BlugeDB) WriteToDB(ctx context.Context,  writes logItem) error {
//	config := bluge.DefaultConfig(b.Name)
//	writer, err := bluge.OpenWriter(config)
//	if err != nil {
//		log.Fatalf("error opening writer: %v", err)
//	}
//	defer writer.Close()
//
//	doc := bluge.NewDocument("example") // can use server name
//
//	for _, label := range writes {
//		doc = doc.AddField(bluge.NewTextField(label.name, label.value))
//	}
//
//	err = writer.Update(doc.ID(), doc)
//	if err != nil {
//		log.Fatalf("error updating document: %v", err)
//	}
//	return err
//}

type TableWrites struct {
	Puts map[string]string
	//deletes map[string]struct{}
}

func (b *BlugeDB) WriteToDB(ctx context.Context, writes TableWrites) error {
	config := bluge.DefaultConfig(b.Folder + "/" + b.Name)
	writer, err := bluge.OpenWriter(config)
	if err != nil {
		log.Fatalf("error opening writer: %v", err)
	}
	defer writer.Close()

	doc := bluge.NewDocument("example") // can use server name

	for key, value := range writes.Puts {
		doc = doc.AddField(bluge.NewTextField(key, value))
	}

	err = writer.Update(doc.ID(), doc)
	if err != nil {
		log.Fatalf("error updating document: %v", err)
	}
	return err
}

// ctx context.Context, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)
func (b *BlugeDB) QueryDB() error {
	config := bluge.DefaultConfig(b.Folder + "/" + b.Name)
	writer, err := bluge.OpenWriter(config)
	reader, err := writer.Reader()
	if err != nil {
		log.Fatalf("error getting index reader: %v", err)
	}
	defer reader.Close()

	q := bluge.NewMatchQuery("1").SetField("foo")
	request := bluge.NewTopNSearch(10, q).
		WithStandardAggregations()
	documentMatchIterator, err := reader.Search(context.Background(), request)
	if err != nil {
		log.Fatalf("error executing search: %v", err)
	}
	match, err := documentMatchIterator.Next()
	for err == nil && match != nil {
		err = match.VisitStoredFields(func(field string, value []byte) bool {
			if field == "_id" {
				fmt.Printf("match: %s\n", string(value))
			}
			return true
		})
		if err != nil {
			log.Fatalf("error loading stored fields: %v", err)
		}
		match, err = documentMatchIterator.Next()
	}
	if err != nil {
		log.Fatalf("error iterator document matches: %v", err)
	}

	return nil
}

func (b *BlugeDB) Close() error {
	return nil
}

func (b *BlugeDB) Path() string {
	return b.Folder + "/" + b.Name
}
