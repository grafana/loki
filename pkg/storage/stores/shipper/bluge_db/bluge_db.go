package bluge_db

import (
	"context"
	"fmt"
	"github.com/blugelabs/bluge"
	"github.com/cortexproject/cortex/pkg/chunk"
	"log"
)

type BlugeDB struct {
	Name   string
	Folder string // snpseg
}

func NewDB(name string, path string) *BlugeDB {
	return &BlugeDB{Name: name, Folder: path} // "./snpseg"
}

type BlugeWriteBatch struct {
	Writes map[string]TableWrites
}

func NewWriteBatch() chunk.WriteBatch {
	return &BlugeWriteBatch{
		Writes: map[string]TableWrites{},
	}
}

func (b *BlugeWriteBatch) getOrCreateTableWrites(tableName string) TableWrites {
	writes, ok := b.Writes[tableName]
	if !ok {
		writes = TableWrites{
			Puts: map[string]string{},
		}
		b.Writes[tableName] = writes
	}

	return writes
}

func (b *BlugeWriteBatch) Delete(tableName, hashValue string, rangeValue []byte) {

}

func (b *BlugeWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	writes := b.getOrCreateTableWrites(tableName)

	key := hashValue
	writes.Puts[key] = string(value)
}

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
		doc = doc.AddField(bluge.NewTextField(key, value).StoreValue())
	}

	err = writer.Update(doc.ID(), doc)
	if err != nil {
		log.Fatalf("error updating document: %v", err)
	}
	return err
}

type IndexQuery struct {
	TableName string
	Matchs    map[string]string // filed => value
}

//visitor segment.StoredFieldVisitor
type StoredFieldVisitor func(field string, value []byte) bool

// ctx context.Context, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)
func (b *BlugeDB) QueryDB(ctx context.Context, query IndexQuery, callback StoredFieldVisitor) error {
	config := bluge.DefaultConfig(b.Folder + "/" + b.Name)
	writer, err := bluge.OpenWriter(config)
	reader, err := writer.Reader()
	if err != nil {
		log.Fatalf("error getting index reader: %v", err)
	}
	defer reader.Close()

	q := bluge.NewMatchQuery("test").SetField("test")
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
