package bluge_db

import (
	"context"
	"github.com/blugelabs/bluge"
	"github.com/cortexproject/cortex/pkg/chunk"
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

func (b *BlugeDB) QueryDB(ctx context.Context, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {

	return nil
}

func (b *BlugeDB) Close() error {
	return nil
}

func (b *BlugeDB) Path() string {
	return b.Folder + "/" + b.Name
}
