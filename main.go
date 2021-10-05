package main

import (
	"context"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	//"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/prometheus/common/model"
	"time"
)

const (
	baseTableName     = "cortex_base"
	tablePrefix       = "cortex_"
	table2Prefix      = "cortex2_"
	chunkTablePrefix  = "chunks_"
	chunkTable2Prefix = "chunks2_"
	tableRetention    = 2 * 7 * 24 * time.Hour
	tablePeriod       = 7 * 24 * time.Hour
	gracePeriod       = 15 * time.Minute
	maxChunkAge       = 12 * time.Hour
	inactiveWrite     = 1
	inactiveRead      = 2
	write             = 200
	read              = 100
	autoScaleLastN    = 2
	autoScaleMin      = 50
	autoScaleMax      = 500
	autoScaleTarget   = 80
)

func main() {
	fsObjectClient, _ := local.NewFSObjectClient(local.FSConfig{Directory: "./obstore"})
	//
	//fmt.Print(err)
	//tb, _ := uploads.NewTable("/Users/mwang5/loki/snpseg", "uploader", fsObjectClient)
	//w := bluge_db.TableWrites{Puts: map[string]string{"foo": "1", "bar": "2"}}
	//tb.Write(context.Background(), w)
	// tb, _ := uploads.LoadTable("/Users/mwang5/loki/snpseg", "uploader", fsObjectClient)
	//tb.Upload(context.Background(), true)

	// /Users/mwang5/loki/dcache/snpseg
	//tb := downloads.NewTable(context.Background(), "snpseg", "/Users/mwang5/loki/dcache", fsObjectClient)
	//tb.Sync(context.Background())

	// bluge_db.BlugeDB

	// build queries each looking for specific value from all the dbs
	//var queries []chunk.IndexQuery
	//for i := 5; i < 8; i++ {
	//	queries = append(queries, chunk.IndexQuery{ValueEqual: []byte(strconv.Itoa(i))})
	//}
	//tb.MultiQueries(context.Background(), queries)

	//cfg := uploads.Config{
	//	CacheDir:     "/Users/mwang5/loki/dcache",
	//	SyncInterval: time.Hour,
	//	CacheTTL:     time.Hour,
	//}

	//cfg := uploads.Config{
	//	Uploader:       "test-table-manager",
	//	IndexDir:       "/Users/mwang5/loki/snpsegindex",
	//	UploadInterval: time.Hour,
	//}
	//
	//tableManager, err := uploads.NewTableManager(cfg, fsObjectClient, nil)
	//
	//wr := bluge_db.NewWriteBatch()
	//wr.Add("mark", "test", []byte("test"), []byte("test"))
	//tableManager.BatchWrite(context.Background(), wr)
	//match := map[string]string{
	//	"test": "test",
	//}
	//qs := bluge_db.IndexQuery{
	//	TableName: "mark",
	//	Matchs:    match,
	//}
	//tableManager.QueryPages(context.Background(), []bluge_db.IndexQuery{qs}, func(field string, value []byte) bool {
	//	if field == "_id" {
	//		fmt.Printf("match: %s\n", string(value))
	//	}
	//	return true
	//})
	//
	config := shipper.Config{
		ActiveIndexDirectory: "snpsegindex",
		SharedStoreType:      "filesystem",
		CacheLocation:        "dcache",
		CacheTTL:             30 * time.Second,
		ResyncInterval:       20 * time.Second,
		IngesterName:         "wang",
		Mode:                 shipper.ModeReadWrite,
	}

	cfg := chunk.DefaultSchemaConfig("", "v10", 0)
	ts, _ := time.Parse(time.RFC3339, "1970-09-16T00:00:00Z")
	tbName := cfg.Configs[0].IndexTables.TableFor(model.TimeFromUnix(ts.Unix()))
	s, _ := shipper.NewShipper(config, fsObjectClient, nil)
	// new struct
	wr := s.NewWriteBatch()
	// create new table 没有对应表明创建
	wr.Add(tbName, "test", []byte("test"), []byte("test"))
	s.BatchWrite(context.Background(), wr)
	//match := map[string]string{
	//	"test": "test",
	//}
	//qs := bluge_db.IndexQuery{
	//	TableName: "mark",
	//	Query:     bluge.NewMatchQuery("test").SetField("test"),
	//}
	//s.QueryPages(context.Background(), []bluge_db.IndexQuery{qs}, func(field string, value []byte) bool {
	//	if field == "_id" {
	//		fmt.Printf("match: %s\n", string(value))
	//	}
	//	fmt.Printf("field: %s\n", string(field))
	//	fmt.Printf("value: %s\n", string(value))
	//	return true
	//})

	// 控制表的生命周期如： 能读写 只读，对应实现es的生命周期
	tbmConfig := chunk.TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		PollInterval:        15 * time.Second,
		IndexTables: chunk.ProvisionConfig{
			ActiveTableProvisionConfig: chunk.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
				//WriteScale:                 activeScalingConfig,
			},
			InactiveTableProvisionConfig: chunk.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
				//InactiveWriteScale:      inactiveScalingConfig,
				InactiveWriteScaleLastN: autoScaleLastN,
			},
		},
		ChunkTables: chunk.ProvisionConfig{
			ActiveTableProvisionConfig: chunk.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: chunk.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
			},
		},
	}
	// MustParseDayTime("2021-09-16")

	//a, _ := cfg.IndexTables..ChunkTableFor(model.TimeFromUnix(ts.Unix()))
	tableClient := shipper.NewBoltDBShipperTableClient(fsObjectClient)
	tableManager, _ := chunk.NewTableManager(tbmConfig, cfg, maxChunkAge, tableClient, nil, nil, nil)

	tableManager.SyncTables(context.Background())
	//tableManager.QueryPages(context.Background(), []bluge_db.IndexQuery{qs}, func(field string, value []byte) bool {
	//	if field == "_id" {
	//		fmt.Printf("match: %s\n", string(value))
	//	}
	//	return true
	//})
	//wr := bluge_db.NewWriteBatch()
	//wr.Add("mark", "test", []byte("test"), []byte("test"))
	// tableManager.BatchWrite(context.Background(), wr)
	//fmt.Print(s)
	select {
	case <-time.Tick(2000000000 * time.Second):
		//t.Fatal("failed to initialize table in time")
	}
	//fmt.Print(tableManager)
}

func MustParseDayTime(s string) chunk.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return chunk.DayTime{model.TimeFromUnix(t.Unix())}
}

//func main() {
//	config := bluge.DefaultConfig("./snpseg")
//	writer, err := bluge.OpenWriter(config)
//	if err != nil {
//		log.Fatalf("error opening writer: %v", err)
//	}
//	defer writer.Close()
//
//	doc := bluge.NewDocument("example").
//		AddField(bluge.NewTextField("name", "bluge"))
//
//	err = writer.Update(doc.ID(), doc)
//	if err != nil {
//		log.Fatalf("error updating document: %v", err)
//	}
//
//	reader, err := writer.Reader()
//	if err != nil {
//		log.Fatalf("error getting index reader: %v", err)
//	}
//	defer reader.Close()
//
//	query := bluge.NewMatchQuery("bluge").SetField("name")
//	request := bluge.NewTopNSearch(10, query).
//		WithStandardAggregations()
//	documentMatchIterator, err := reader.Search(context.Background(), request)
//	if err != nil {
//		log.Fatalf("error executing search: %v", err)
//	}
//	match, err := documentMatchIterator.Next()
//	for err == nil && match != nil {
//		err = match.VisitStoredFields(func(field string, value []byte) bool {
//			if field == "_id" {
//				fmt.Printf("match: %s\n", string(value))
//			}
//			return true
//		})
//		if err != nil {
//			log.Fatalf("error loading stored fields: %v", err)
//		}
//		match, err = documentMatchIterator.Next()
//	}
//	if err != nil {
//		log.Fatalf("error iterator document matches: %v", err)
//	}
//
//	// tar + gzip
//	var buf bytes.Buffer
//	compress("./snpseg", &buf)
//
//	// write the .tar.gzip
//	fileToWrite, err := os.OpenFile("./compress.tar.gzip", os.O_CREATE|os.O_RDWR, os.FileMode(600))
//	if err != nil {
//		panic(err)
//	}
//	if _, err := io.Copy(fileToWrite, &buf); err != nil {
//		panic(err)
//	}
//
//	content, err := ioutil.ReadFile("obstore2")
//	red := bytes.NewReader(content)
//	// untar write
//	if err := decompress(red, "./uncompressHere/"); err != nil {
//		// probably delete uncompressHere?
//	}
//
//	content, err = ioutil.ReadFile("compress.tar.gzip")
//	fsObjectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: "./obstore"})
//	fsObjectClient.PutObject(context.Background(), "path.Join(folder, fileName)2", bytes.NewReader(content))
//
//	pwd, _ := os.Getwd()
//	err = shipper_util.GetFileFromStorage(context.Background(), fsObjectClient, "path.Join(folder, fileName)2", pwd+"/obstore2")
//}
//
//func compress(src string, buf io.Writer) error {
//	// tar > gzip > buf
//	zr := gzip.NewWriter(buf)
//	tw := tar.NewWriter(zr)
//
//	// is file a folder?
//	fi, err := os.Stat(src)
//	if err != nil {
//		return err
//	}
//	mode := fi.Mode()
//	if mode.IsRegular() {
//		// get header
//		header, err := tar.FileInfoHeader(fi, src)
//		if err != nil {
//			return err
//		}
//		// write header
//		if err := tw.WriteHeader(header); err != nil {
//			return err
//		}
//		// get content
//		data, err := os.Open(src)
//		if err != nil {
//			return err
//		}
//		if _, err := io.Copy(tw, data); err != nil {
//			return err
//		}
//	} else if mode.IsDir() { // folder
//
//		// walk through every file in the folder
//		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
//			// generate tar header
//			header, err := tar.FileInfoHeader(fi, file)
//			if err != nil {
//				return err
//			}
//
//			// must provide real name
//			// (see https://golang.org/src/archive/tar/common.go?#L626)
//			header.Name = filepath.ToSlash(file)
//
//			// write header
//			if err := tw.WriteHeader(header); err != nil {
//				return err
//			}
//			// if not a dir, write file content
//			if !fi.IsDir() {
//				data, err := os.Open(file)
//				if err != nil {
//					return err
//				}
//				if _, err := io.Copy(tw, data); err != nil {
//					return err
//				}
//			}
//			return nil
//		})
//	} else {
//		return fmt.Errorf("error: file type not supported")
//	}
//
//	// produce tar
//	if err := tw.Close(); err != nil {
//		return err
//	}
//	// produce gzip
//	if err := zr.Close(); err != nil {
//		return err
//	}
//	//
//	return nil
//}
//
//// check for path traversal and correct forward slashes
//func validRelPath(p string) bool {
//	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
//		return false
//	}
//	return true
//}
//
//func decompress(src io.Reader, dst string) error {
//	// ungzip
//	zr, err := gzip.NewReader(src)
//	if err != nil {
//		return err
//	}
//	// untar
//	tr := tar.NewReader(zr)
//
//	// uncompress each element
//	for {
//		header, err := tr.Next()
//		if err == io.EOF {
//			break // End of archive
//		}
//		if err != nil {
//			return err
//		}
//		target := header.Name
//
//		// validate name against path traversal
//		if !validRelPath(header.Name) {
//			return fmt.Errorf("tar contained invalid name error %q", target)
//		}
//
//		// add dst + re-format slashes according to system
//		target = filepath.Join(dst, header.Name)
//		// if no join is needed, replace with ToSlash:
//		// target = filepath.ToSlash(header.Name)
//
//		// check the type
//		switch header.Typeflag {
//
//		// if its a dir and it doesn't exist create it (with 0755 permission)
//		case tar.TypeDir:
//			if _, err := os.Stat(target); err != nil {
//				if err := os.MkdirAll(target, 0755); err != nil {
//					return err
//				}
//			}
//		// if it's a file create it (with same permission)
//		case tar.TypeReg:
//			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
//			if err != nil {
//				return err
//			}
//			// copy over contents
//			if _, err := io.Copy(fileToWrite, tr); err != nil {
//				return err
//			}
//			// manually close here after each file operation; defering would cause each file close
//			// to wait until all operations have completed.
//			fileToWrite.Close()
//		}
//	}
//
//	//
//	return nil
//}
//
////func decompress(src io.Reader, dst string) error {
////	// ungzip
////	zr, err := gzip.NewReader(src)
////	if err != nil {
////		return err
////	}
////	// untar
////	tr := tar.NewReader(zr)
////
////	// uncompress each element
////	for {
////		header, err := tr.Next()
////		if err == io.EOF {
////			break // End of archive
////		}
////		if err != nil {
////			return err
////		}
////		target := header.Name
////
////		// validate name against path traversal
////		if !validRelPath(header.Name) {
////			return fmt.Errorf("tar contained invalid name error %q", target)
////		}
////
////		// add dst + re-format slashes according to system
////		target = filepath.Join(dst, header.Name)
////		// if no join is needed, replace with ToSlash:
////		// target = filepath.ToSlash(header.Name)
////
////		// check the type
////		switch header.Typeflag {
////
////		// if its a dir and it doesn't exist create it (with 0755 permission)
////		case tar.TypeDir:
////			if _, err := os.Stat(target); err != nil {
////				if err := os.MkdirAll(target, 0755); err != nil {
////					return err
////				}
////			}
////		// if it's a file create it (with same permission)
////		case tar.TypeReg:
////			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
////			if err != nil {
////				return err
////			}
////			// copy over contents
////			if _, err := io.Copy(fileToWrite, tr); err != nil {
////				return err
////			}
////			// manually close here after each file operation; defering would cause each file close
////			// to wait until all operations have completed.
////			fileToWrite.Close()
////		}
////	}
////
////	//
////	return nil
////}
