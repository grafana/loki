package baidubce

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/storage/chunk"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
)

// ObjectNotFoundErr The specified key does not exist. ref: https://cloud.baidu.com/doc/BOS/s/Ajwvysfpl
const ObjectNotFoundErr = "ObjectNotFoundErr"

const DefaultEndpoint = "bj.bcebos.com"
const DefaultStsTokenRefreshPeriod = 5 * time.Minute

var bosRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "loki",
	Name:      "bos_request_duration_seconds",
	Help:      "Time spent doing bos requests.",
	Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
}, []string{"operation", "status_code"}))

func init() {
	bosRequestDuration.Register()
}

type BosStorageConfig struct {
	BucketName      string `yaml:"bucket_name"`
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id,omitempty"`
	SecretAccessKey string `yaml:"secret_access_key,omitempty"`
	// StsTokenPath Once this is enabled, AccessKeyID SecretAccessKey will be an invalid value
	StsTokenPath string `yaml:"sts_token_path,omitempty"`
	// StsTokenRefreshPeriod Time to refresh StsToken,If StsTokenPath is empty, this value is invalid
	// If StsTokenPath is file path, this value is invalid StsToken will be refreshed every time when the file is modified
	StsTokenRefreshPeriod time.Duration `yaml:"sts_token_refresh_period,omitempty"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *BosStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *BosStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"baidubce.bucket-name", "", "Name of Bos bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"baidubce.endpoint", DefaultEndpoint, "Bos endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"baidubce.access-key-id", "", "Baidu BCE Access Key ID,It is abandoned when using STS.")
	f.StringVar(&cfg.SecretAccessKey, prefix+"baidubce.secret-access-key", "", "Baidu BCE Secret Access Key,It is abandoned when using STS.")
	f.StringVar(&cfg.StsTokenPath, prefix+"baidubce.sts-token-path", "", "Must be an HTTP address(must start with `http://` prefix) or file address where authentication can be obtained.")
	f.DurationVar(&cfg.StsTokenRefreshPeriod, prefix+"baidubce.sts-token-refresh-period", DefaultStsTokenRefreshPeriod, "Time to refresh STS token.")
}

type SessionToken struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	CreateTime      string `json:"create_time,omitempty"`
	Expiration      string `json:"expiration,omitempty"`
	UserID          string `json:"user_id,omitempty"`
}

type BosObjectStorage struct {
	cfg    *BosStorageConfig
	client *bos.Client
}

func (cfg *BosStorageConfig) Validate() error {
	return nil
}

func (b *BosObjectStorage) startStsTokenReFresh() {
	if strings.HasPrefix(b.cfg.StsTokenPath, "http://") {
		timeTicker := time.NewTicker(b.cfg.StsTokenRefreshPeriod)
		for {
			err := b.refreshStsClient()
			if err != nil {
				continue
			}
			<-timeTicker.C
		}
	}
	// If StsTokenPath is file path
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op == fsnotify.Remove {
					watcher.Remove(event.Name)
					watcher.Add(b.cfg.StsTokenPath)
					err := b.refreshStsClient()
					if err != nil {
						continue
					}
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					err := b.refreshStsClient()
					if err != nil {
						continue
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	err = watcher.Add(b.cfg.StsTokenPath)
	if err != nil {
		log.Fatal(err)
	}

	<-done
}

func NewBosObjectStorage(cfg *BosStorageConfig) (*BosObjectStorage, error) {
	if cfg.StsTokenPath == "" {
		clientConfig := bos.BosClientConfiguration{
			Ak:               cfg.AccessKeyID,
			Sk:               cfg.SecretAccessKey,
			Endpoint:         cfg.Endpoint,
			RedirectDisabled: false,
		}
		bosClient, err := bos.NewClientWithConfig(&clientConfig)
		if err != nil {
			return nil, err
		}
		return &BosObjectStorage{
			cfg:    cfg,
			client: bosClient,
		}, nil
	}
	bosObjectStorage := &BosObjectStorage{
		cfg: cfg,
	}
	// when first created check the current Sts
	err := bosObjectStorage.refreshStsClient()
	if err != nil {
		return nil, err
	}
	// start a goroutine to refresh the Sts
	go bosObjectStorage.startStsTokenReFresh()
	return bosObjectStorage, nil
}

func (b *BosObjectStorage) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "Bos.PutObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		body, err := bce.NewBodyFromSizedReader(object, -1)
		if err != nil {
			return err
		}
		_, err = b.client.BasicPutObject(b.cfg.BucketName, objectKey, body)
		return err
	})
}

func (b *BosObjectStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var res *api.GetObjectResult
	err := instrument.CollectedRequest(ctx, "Bos.GetObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var requestErr error
		res, requestErr = b.client.BasicGetObject(b.cfg.BucketName, objectKey)
		return requestErr
	})
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get bos object")
	}
	var size int64
	if res.ContentLength != 0 {
		size = res.ContentLength
	}
	return res.Body, size, nil
}

func (b *BosObjectStorage) List(ctx context.Context, prefix string, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	var storageObjects []chunk.StorageObject
	var commonPrefixes []chunk.StorageCommonPrefix

	err := instrument.CollectedRequest(ctx, "Bos.List", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		args := new(api.ListObjectsArgs)
		args.Prefix = prefix
		args.Delimiter = delimiter
		for {
			listObjectResult, err := b.client.ListObjects(b.cfg.BucketName, args)
			if err != nil {
				return err
			}
			for _, content := range listObjectResult.Contents {
				// LastModified format 2021-10-28T06:55:01Z
				LastModifiedTime, err := time.Parse(time.RFC3339, content.LastModified)
				if err != nil {
					return err
				}
				storageObjects = append(storageObjects, chunk.StorageObject{
					Key:        content.Key,
					ModifiedAt: LastModifiedTime,
				})
			}
			for _, commonPrefix := range listObjectResult.CommonPrefixes {
				commonPrefixes = append(commonPrefixes, chunk.StorageCommonPrefix(commonPrefix.Prefix))
			}
			if !listObjectResult.IsTruncated {
				break
			}
			args.Prefix = listObjectResult.Prefix
			args.Marker = listObjectResult.NextMarker
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return storageObjects, commonPrefixes, nil
}

func (b *BosObjectStorage) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "Bos.DeleteObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		err := b.client.DeleteObject(b.cfg.BucketName, objectKey)
		return err
	})
}

func (b *BosObjectStorage) IsObjectNotFoundErr(err error) bool {
	// ref: https://cloud.baidu.com/doc/BOS/s/rjwvyrwye
	switch realErr := err.(type) {
	case *bce.BceClientError:
		return false
	case *bce.BceServiceError:
		if realErr.Code == ObjectNotFoundErr {
			return true
		}
	default:
		return false
	}
	return false
}

func (b *BosObjectStorage) Stop() {}

func getSts(stsTokenPath string) (SessionToken, error) {
	if strings.HasPrefix(stsTokenPath, "http://") {
		resp, err := http.Get(stsTokenPath)
		if err != nil {
			return SessionToken{}, err
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		var sessionToken SessionToken
		err = json.Unmarshal(body, &sessionToken)
		if err != nil {
			return SessionToken{}, err
		}
		return sessionToken, nil
	}
	// If StsTokenPath is file path
	body, err := ioutil.ReadFile(stsTokenPath)
	if err != nil {
		return SessionToken{}, err
	}
	var sessionToken SessionToken
	err = json.Unmarshal(body, &sessionToken)
	if err != nil {
		return SessionToken{}, err
	}
	return sessionToken, nil
}

func buildStsBosClient(sts SessionToken, endPoint string) (*bos.Client, error) {
	bosClient, err := bos.NewClient(sts.AccessKeyID, sts.SecretAccessKey, endPoint)
	if err != nil {
		return nil, err
	}
	stsCredential, err := auth.NewSessionBceCredentials(
		sts.AccessKeyID,
		sts.SecretAccessKey,
		sts.SessionToken)
	if err != nil {
		return nil, err
	}
	bosClient.Config.Credentials = stsCredential
	return bosClient, nil
}

func (b *BosObjectStorage) refreshStsClient() error {
	return instrument.CollectedRequest(context.Background(), "Bos.refreshStsClient", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		sts, err := getSts(b.cfg.StsTokenPath)
		if err != nil {
			return err
		}
		stsBosClient, err := buildStsBosClient(sts, b.cfg.Endpoint)
		if err != nil {
			return err
		}
		b.client = stsBosClient
		return nil
	})
}
