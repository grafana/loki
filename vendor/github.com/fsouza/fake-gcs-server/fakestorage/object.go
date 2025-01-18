// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/internal/backend"
	"github.com/fsouza/fake-gcs-server/internal/notification"
	"github.com/gorilla/mux"
)

var errInvalidGeneration = errors.New("invalid generation ID")

// ObjectAttrs returns only the meta-data about an object without its contents.
type ObjectAttrs struct {
	BucketName         string
	Name               string
	Size               int64
	StorageClass       string
	ContentType        string
	ContentEncoding    string
	ContentDisposition string
	ContentLanguage    string
	CacheControl       string
	// Crc32c checksum of Content. calculated by server when it's upload methods are used.
	Crc32c  string
	Md5Hash string
	Etag    string
	ACL     []storage.ACLRule
	// Dates and generation can be manually injected, so you can do assertions on them,
	// or let us fill these fields for you
	Created    time.Time
	Updated    time.Time
	Deleted    time.Time
	CustomTime time.Time
	Generation int64
	Metadata   map[string]string
}

func (o *ObjectAttrs) id() string {
	return o.BucketName + "/" + o.Name
}

type jsonObject struct {
	BucketName         string            `json:"bucket"`
	Name               string            `json:"name"`
	Size               int64             `json:"size,string"`
	StorageClass       string            `json:"storageClass"`
	ContentType        string            `json:"contentType"`
	ContentEncoding    string            `json:"contentEncoding"`
	ContentDisposition string            `json:"contentDisposition"`
	ContentLanguage    string            `json:"contentLanguage"`
	Crc32c             string            `json:"crc32c,omitempty"`
	Md5Hash            string            `json:"md5Hash,omitempty"`
	Etag               string            `json:"etag,omitempty"`
	ACL                []aclRule         `json:"acl,omitempty"`
	Created            time.Time         `json:"created,omitempty"`
	Updated            time.Time         `json:"updated,omitempty"`
	Deleted            time.Time         `json:"deleted,omitempty"`
	CustomTime         time.Time         `json:"customTime,omitempty"`
	Generation         int64             `json:"generation,omitempty,string"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// MarshalJSON for ObjectAttrs to use ACLRule instead of storage.ACLRule
func (o ObjectAttrs) MarshalJSON() ([]byte, error) {
	temp := jsonObject{
		BucketName:         o.BucketName,
		Name:               o.Name,
		StorageClass:       o.StorageClass,
		ContentType:        o.ContentType,
		ContentEncoding:    o.ContentEncoding,
		ContentDisposition: o.ContentDisposition,
		ContentLanguage:    o.ContentLanguage,
		Size:               o.Size,
		Crc32c:             o.Crc32c,
		Md5Hash:            o.Md5Hash,
		Etag:               o.Etag,
		Created:            o.Created,
		Updated:            o.Updated,
		Deleted:            o.Deleted,
		CustomTime:         o.CustomTime,
		Generation:         o.Generation,
		Metadata:           o.Metadata,
	}
	temp.ACL = make([]aclRule, len(o.ACL))
	for i, ACL := range o.ACL {
		temp.ACL[i] = aclRule(ACL)
	}
	return json.Marshal(temp)
}

// UnmarshalJSON for ObjectAttrs to use ACLRule instead of storage.ACLRule
func (o *ObjectAttrs) UnmarshalJSON(data []byte) error {
	var temp jsonObject
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	o.BucketName = temp.BucketName
	o.Name = temp.Name
	o.StorageClass = temp.StorageClass
	o.ContentType = temp.ContentType
	o.ContentEncoding = temp.ContentEncoding
	o.ContentDisposition = temp.ContentDisposition
	o.ContentLanguage = temp.ContentLanguage
	o.Size = temp.Size
	o.Crc32c = temp.Crc32c
	o.Md5Hash = temp.Md5Hash
	o.Etag = temp.Etag
	o.Created = temp.Created
	o.Updated = temp.Updated
	o.Deleted = temp.Deleted
	o.Generation = temp.Generation
	o.Metadata = temp.Metadata
	o.CustomTime = temp.CustomTime
	o.ACL = make([]storage.ACLRule, len(temp.ACL))
	for i, ACL := range temp.ACL {
		o.ACL[i] = storage.ACLRule(ACL)
	}

	return nil
}

// Object represents an object that is stored within the fake server. The
// content of this type is stored is buffered, i.e. it's stored in memory.
// Use StreamingObject to stream the content from a reader, e.g a file.
type Object struct {
	ObjectAttrs
	Content []byte `json:"-"`
}

type noopSeekCloser struct {
	io.ReadSeeker
}

func (n noopSeekCloser) Close() error {
	return nil
}

func (o Object) StreamingObject() StreamingObject {
	return StreamingObject{
		ObjectAttrs: o.ObjectAttrs,
		Content:     noopSeekCloser{bytes.NewReader(o.Content)},
	}
}

// StreamingObject is the streaming version of Object.
type StreamingObject struct {
	ObjectAttrs
	Content io.ReadSeekCloser `json:"-"`
}

func (o *StreamingObject) Close() error {
	if o != nil && o.Content != nil {
		return o.Content.Close()
	}
	return nil
}

func (o *StreamingObject) BufferedObject() (Object, error) {
	data, err := io.ReadAll(o.Content)
	return Object{
		ObjectAttrs: o.ObjectAttrs,
		Content:     data,
	}, err
}

// ACLRule is an alias of storage.ACLRule to have custom JSON marshal
type aclRule storage.ACLRule

// ProjectTeam is an alias of storage.ProjectTeam to have custom JSON marshal
type projectTeam storage.ProjectTeam

// MarshalJSON for ACLRule to customize field names
func (acl aclRule) MarshalJSON() ([]byte, error) {
	temp := struct {
		Entity      storage.ACLEntity `json:"entity"`
		EntityID    string            `json:"entityId"`
		Role        storage.ACLRole   `json:"role"`
		Domain      string            `json:"domain"`
		Email       string            `json:"email"`
		ProjectTeam *projectTeam      `json:"projectTeam"`
	}{
		Entity:      acl.Entity,
		EntityID:    acl.EntityID,
		Role:        acl.Role,
		Domain:      acl.Domain,
		Email:       acl.Email,
		ProjectTeam: (*projectTeam)(acl.ProjectTeam),
	}
	return json.Marshal(temp)
}

// UnmarshalJSON for ACLRule to customize field names
func (acl *aclRule) UnmarshalJSON(data []byte) error {
	temp := struct {
		Entity      storage.ACLEntity `json:"entity"`
		EntityID    string            `json:"entityId"`
		Role        storage.ACLRole   `json:"role"`
		Domain      string            `json:"domain"`
		Email       string            `json:"email"`
		ProjectTeam *projectTeam      `json:"projectTeam"`
	}{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	acl.Entity = temp.Entity
	acl.EntityID = temp.EntityID
	acl.Role = temp.Role
	acl.Domain = temp.Domain
	acl.Email = temp.Email
	acl.ProjectTeam = (*storage.ProjectTeam)(temp.ProjectTeam)
	return nil
}

// MarshalJSON for ProjectTeam to customize field names
func (team projectTeam) MarshalJSON() ([]byte, error) {
	temp := struct {
		ProjectNumber string `json:"projectNumber"`
		Team          string `json:"team"`
	}{
		ProjectNumber: team.ProjectNumber,
		Team:          team.Team,
	}
	return json.Marshal(temp)
}

// UnmarshalJSON for ProjectTeam to customize field names
func (team *projectTeam) UnmarshalJSON(data []byte) error {
	temp := struct {
		ProjectNumber string `json:"projectNumber"`
		Team          string `json:"team"`
	}{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	team.ProjectNumber = temp.ProjectNumber
	team.Team = temp.Team
	return nil
}

// CreateObject is the non-streaming version of CreateObjectStreaming.
//
// In addition to streaming, CreateObjectStreaming returns an error instead of
// panicking when an error occurs.
func (s *Server) CreateObject(obj Object) {
	err := s.CreateObjectStreaming(obj.StreamingObject())
	if err != nil {
		panic(err)
	}
}

// CreateObjectStreaming stores the given object internally.
//
// If the bucket within the object doesn't exist, it also creates it. If the
// object already exists, it overwrites the object.
func (s *Server) CreateObjectStreaming(obj StreamingObject) error {
	obj, err := s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return err
	}
	obj.Close()
	return nil
}

func (s *Server) createObject(obj StreamingObject, conditions backend.Conditions) (StreamingObject, error) {
	oldBackendObj, err := s.backend.GetObject(obj.BucketName, obj.Name)
	// Calling Close before checking err is okay on objects, and the object
	// may need to be closed whether or not there's an error.
	defer oldBackendObj.Close() //lint:ignore SA5001 // see above

	prevVersionExisted := err == nil

	// The caller is responsible for closing the created object.
	newBackendObj, err := s.backend.CreateObject(toBackendObjects([]StreamingObject{obj})[0], conditions)
	if err != nil {
		return StreamingObject{}, err
	}

	var newObjEventAttr map[string]string
	if prevVersionExisted {
		newObjEventAttr = map[string]string{
			"overwroteGeneration": strconv.FormatInt(oldBackendObj.Generation, 10),
		}

		oldObjEventAttr := map[string]string{
			"overwrittenByGeneration": strconv.FormatInt(newBackendObj.Generation, 10),
		}

		bucket, _ := s.backend.GetBucket(obj.BucketName)
		if bucket.VersioningEnabled {
			s.eventManager.Trigger(&oldBackendObj, notification.EventArchive, oldObjEventAttr)
		} else {
			s.eventManager.Trigger(&oldBackendObj, notification.EventDelete, oldObjEventAttr)
		}
	}

	newObj := fromBackendObjects([]backend.StreamingObject{newBackendObj})[0]
	s.eventManager.Trigger(&newBackendObj, notification.EventFinalize, newObjEventAttr)
	return newObj, nil
}

type ListOptions struct {
	Prefix                   string
	Delimiter                string
	Versions                 bool
	StartOffset              string
	EndOffset                string
	IncludeTrailingDelimiter bool
	MaxResults               int
}

// ListObjects returns a sorted list of objects that match the given criteria,
// or an error if the bucket doesn't exist.
//
// Deprecated: use ListObjectsWithOptions.
func (s *Server) ListObjects(bucketName, prefix, delimiter string, versions bool) ([]ObjectAttrs, []string, error) {
	return s.ListObjectsWithOptions(bucketName, ListOptions{
		Prefix:    prefix,
		Delimiter: delimiter,
		Versions:  versions,
	})
}

func (s *Server) ListObjectsWithOptions(bucketName string, options ListOptions) ([]ObjectAttrs, []string, error) {
	backendObjects, err := s.backend.ListObjects(bucketName, options.Prefix, options.Versions)
	if err != nil {
		return nil, nil, err
	}
	objects := fromBackendObjectsAttrs(backendObjects)
	slices.SortFunc(objects, func(left, right ObjectAttrs) int {
		return strings.Compare(left.Name, right.Name)
	})
	var respObjects []ObjectAttrs
	prefixes := make(map[string]bool)
	for _, obj := range objects {
		if !strings.HasPrefix(obj.Name, options.Prefix) {
			continue
		}
		objName := strings.Replace(obj.Name, options.Prefix, "", 1)
		delimPos := strings.Index(objName, options.Delimiter)
		if options.Delimiter != "" && delimPos > -1 {
			prefix := obj.Name[:len(options.Prefix)+delimPos+1]
			if isInOffset(prefix, options.StartOffset, options.EndOffset) {
				prefixes[prefix] = true
			}
			if options.IncludeTrailingDelimiter && obj.Name == prefix {
				respObjects = append(respObjects, obj)
			}
		} else {
			if isInOffset(obj.Name, options.StartOffset, options.EndOffset) {
				respObjects = append(respObjects, obj)
			}
		}
	}
	respPrefixes := make([]string, 0, len(prefixes))
	for p := range prefixes {
		respPrefixes = append(respPrefixes, p)
	}
	sort.Strings(respPrefixes)
	if options.MaxResults != 0 && len(respObjects) > options.MaxResults {
		respObjects = respObjects[:options.MaxResults]
	}
	return respObjects, respPrefixes, nil
}

func isInOffset(name, startOffset, endOffset string) bool {
	if endOffset != "" && startOffset != "" {
		return strings.Compare(name, endOffset) < 0 && strings.Compare(name, startOffset) >= 0
	} else if endOffset != "" {
		return strings.Compare(name, endOffset) < 0
	} else if startOffset != "" {
		return strings.Compare(name, startOffset) >= 0
	} else {
		return true
	}
}

func getCurrentIfZero(date time.Time) time.Time {
	if date.IsZero() {
		return time.Now()
	}
	return date
}

func toBackendObjects(objects []StreamingObject) []backend.StreamingObject {
	backendObjects := make([]backend.StreamingObject, 0, len(objects))
	for _, o := range objects {
		backendObjects = append(backendObjects, backend.StreamingObject{
			ObjectAttrs: backend.ObjectAttrs{
				BucketName:         o.BucketName,
				Name:               o.Name,
				StorageClass:       o.StorageClass,
				ContentType:        o.ContentType,
				ContentEncoding:    o.ContentEncoding,
				ContentDisposition: o.ContentDisposition,
				ContentLanguage:    o.ContentLanguage,
				CacheControl:       o.CacheControl,
				ACL:                o.ACL,
				Created:            getCurrentIfZero(o.Created).Format(timestampFormat),
				Deleted:            o.Deleted.Format(timestampFormat),
				Updated:            getCurrentIfZero(o.Updated).Format(timestampFormat),
				CustomTime:         o.CustomTime.Format(timestampFormat),
				Generation:         o.Generation,
				Metadata:           o.Metadata,
			},
			Content: o.Content,
		})
	}
	return backendObjects
}

func bufferedObjectsToBackendObjects(objects []Object) []backend.StreamingObject {
	backendObjects := make([]backend.StreamingObject, 0, len(objects))
	for _, bufferedObject := range objects {
		o := bufferedObject.StreamingObject()
		backendObjects = append(backendObjects, backend.StreamingObject{
			ObjectAttrs: backend.ObjectAttrs{
				BucketName:         o.BucketName,
				Name:               o.Name,
				StorageClass:       o.StorageClass,
				ContentType:        o.ContentType,
				ContentEncoding:    o.ContentEncoding,
				ContentDisposition: o.ContentDisposition,
				ContentLanguage:    o.ContentLanguage,
				ACL:                o.ACL,
				Created:            getCurrentIfZero(o.Created).Format(timestampFormat),
				Deleted:            o.Deleted.Format(timestampFormat),
				Updated:            getCurrentIfZero(o.Updated).Format(timestampFormat),
				CustomTime:         o.CustomTime.Format(timestampFormat),
				Generation:         o.Generation,
				Metadata:           o.Metadata,
				Crc32c:             o.Crc32c,
				Md5Hash:            o.Md5Hash,
				Size:               o.Size,
				Etag:               o.Etag,
			},
			Content: o.Content,
		})
	}
	return backendObjects
}

func fromBackendObjects(objects []backend.StreamingObject) []StreamingObject {
	backendObjects := make([]StreamingObject, 0, len(objects))
	for _, o := range objects {
		backendObjects = append(backendObjects, StreamingObject{
			ObjectAttrs: ObjectAttrs{
				BucketName:         o.BucketName,
				Name:               o.Name,
				Size:               o.Size,
				StorageClass:       o.StorageClass,
				ContentType:        o.ContentType,
				ContentEncoding:    o.ContentEncoding,
				ContentDisposition: o.ContentDisposition,
				ContentLanguage:    o.ContentLanguage,
				CacheControl:       o.CacheControl,
				Crc32c:             o.Crc32c,
				Md5Hash:            o.Md5Hash,
				Etag:               o.Etag,
				ACL:                o.ACL,
				Created:            convertTimeWithoutError(o.Created),
				Deleted:            convertTimeWithoutError(o.Deleted),
				Updated:            convertTimeWithoutError(o.Updated),
				CustomTime:         convertTimeWithoutError(o.CustomTime),
				Generation:         o.Generation,
				Metadata:           o.Metadata,
			},
			Content: o.Content,
		})
	}
	return backendObjects
}

func fromBackendObjectsAttrs(objectAttrs []backend.ObjectAttrs) []ObjectAttrs {
	oattrs := make([]ObjectAttrs, 0, len(objectAttrs))
	for _, o := range objectAttrs {
		oattrs = append(oattrs, ObjectAttrs{
			BucketName:         o.BucketName,
			Name:               o.Name,
			Size:               o.Size,
			StorageClass:       o.StorageClass,
			ContentType:        o.ContentType,
			ContentEncoding:    o.ContentEncoding,
			ContentDisposition: o.ContentDisposition,
			ContentLanguage:    o.ContentLanguage,
			CacheControl:       o.CacheControl,
			Crc32c:             o.Crc32c,
			Md5Hash:            o.Md5Hash,
			Etag:               o.Etag,
			ACL:                o.ACL,
			Created:            convertTimeWithoutError(o.Created),
			Deleted:            convertTimeWithoutError(o.Deleted),
			Updated:            convertTimeWithoutError(o.Updated),
			CustomTime:         convertTimeWithoutError(o.CustomTime),
			Generation:         o.Generation,
			Metadata:           o.Metadata,
		})
	}
	return oattrs
}

func convertTimeWithoutError(t string) time.Time {
	r, _ := time.Parse(timestampFormat, t)
	return r
}

// GetObject is the non-streaming version of GetObjectStreaming.
func (s *Server) GetObject(bucketName, objectName string) (Object, error) {
	streamingObject, err := s.GetObjectStreaming(bucketName, objectName)
	if err != nil {
		return Object{}, err
	}
	return streamingObject.BufferedObject()
}

// GetObjectStreaming returns the object with the given name in the given
// bucket, or an error if the object doesn't exist.
func (s *Server) GetObjectStreaming(bucketName, objectName string) (StreamingObject, error) {
	backendObj, err := s.backend.GetObject(bucketName, objectName)
	if err != nil {
		return StreamingObject{}, err
	}
	obj := fromBackendObjects([]backend.StreamingObject{backendObj})[0]
	return obj, nil
}

// GetObjectWithGeneration is the non-streaming version of
// GetObjectWithGenerationStreaming.
func (s *Server) GetObjectWithGeneration(bucketName, objectName string, generation int64) (Object, error) {
	streamingObject, err := s.GetObjectWithGenerationStreaming(bucketName, objectName, generation)
	if err != nil {
		return Object{}, err
	}
	return streamingObject.BufferedObject()
}

// GetObjectWithGenerationStreaming returns the object with the given name and
// given generation ID in the given bucket, or an error if the object doesn't
// exist.
//
// If versioning is enabled, archived versions are considered.
func (s *Server) GetObjectWithGenerationStreaming(bucketName, objectName string, generation int64) (StreamingObject, error) {
	backendObj, err := s.backend.GetObjectWithGeneration(bucketName, objectName, generation)
	if err != nil {
		return StreamingObject{}, err
	}
	obj := fromBackendObjects([]backend.StreamingObject{backendObj})[0]
	return obj, nil
}

func (s *Server) objectWithGenerationOnValidGeneration(bucketName, objectName, generationStr string) (StreamingObject, error) {
	generation, err := strconv.ParseInt(generationStr, 10, 64)
	if err != nil && generationStr != "" {
		return StreamingObject{}, errInvalidGeneration
	} else if generation > 0 {
		return s.GetObjectWithGenerationStreaming(bucketName, objectName, generation)
	}
	return s.GetObjectStreaming(bucketName, objectName)
}

func (s *Server) listObjects(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]
	var maxResults int
	var err error
	if maxResultsStr := r.URL.Query().Get("maxResults"); maxResultsStr != "" {
		maxResults, err = strconv.Atoi(maxResultsStr)
		if err != nil {
			return jsonResponse{status: http.StatusBadRequest}
		}
	}
	objs, prefixes, err := s.ListObjectsWithOptions(bucketName, ListOptions{
		Prefix:                   r.URL.Query().Get("prefix"),
		Delimiter:                r.URL.Query().Get("delimiter"),
		Versions:                 r.URL.Query().Get("versions") == "true",
		StartOffset:              r.URL.Query().Get("startOffset"),
		EndOffset:                r.URL.Query().Get("endOffset"),
		IncludeTrailingDelimiter: r.URL.Query().Get("includeTrailingDelimiter") == "true",
		MaxResults:               maxResults,
	})
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	return jsonResponse{data: newListObjectsResponse(objs, prefixes, s.externalURL)}
}

func (s *Server) xmlListObjects(r *http.Request) xmlResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]

	opts := ListOptions{
		Prefix:    r.URL.Query().Get("prefix"),
		Delimiter: r.URL.Query().Get("delimiter"),
		Versions:  r.URL.Query().Get("versions") == "true",
	}

	objs, prefixes, err := s.ListObjectsWithOptions(bucketName, opts)
	if err != nil {
		return xmlResponse{
			status:       http.StatusInternalServerError,
			errorMessage: err.Error(),
		}
	}

	result := ListBucketResult{
		Name:      bucketName,
		Delimiter: opts.Delimiter,
		Prefix:    opts.Prefix,
		KeyCount:  len(objs),
	}

	if opts.Delimiter != "" {
		for _, prefix := range prefixes {
			result.CommonPrefixes = append(result.CommonPrefixes, CommonPrefix{Prefix: prefix})
		}
	}

	for _, obj := range objs {
		result.Contents = append(result.Contents, Contents{
			Key:          obj.Name,
			Generation:   obj.Generation,
			Size:         obj.Size,
			LastModified: obj.Updated.Format(time.RFC3339),
			ETag:         ETag{Value: obj.Etag},
		})
	}

	raw, err := xml.Marshal(result)
	if err != nil {
		return xmlResponse{
			status:       http.StatusInternalServerError,
			errorMessage: err.Error(),
		}
	}

	return xmlResponse{
		status: http.StatusOK,
		data:   []byte(xml.Header + string(raw)),
	}
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request) {
	if alt := r.URL.Query().Get("alt"); alt == "media" || r.Method == http.MethodHead {
		s.downloadObject(w, r)
		return
	}

	handler := jsonToHTTPHandler(func(r *http.Request) jsonResponse {
		vars := unescapeMuxVars(mux.Vars(r))

		projection := storage.ProjectionNoACL
		if r.URL.Query().Has("projection") {
			switch value := strings.ToLower(r.URL.Query().Get("projection")); value {
			case "full":
				projection = storage.ProjectionFull
			case "noacl":
				projection = storage.ProjectionNoACL
			default:
				return jsonResponse{
					status:       http.StatusBadRequest,
					errorMessage: fmt.Sprintf("invalid projection: %q", value),
				}
			}
		}

		obj, err := s.objectWithGenerationOnValidGeneration(vars["bucketName"], vars["objectName"], r.FormValue("generation"))
		// Calling Close before checking err is okay on objects, and the object
		// may need to be closed whether or not there's an error.
		defer obj.Close() //lint:ignore SA5001 // see above
		if err != nil {
			statusCode := http.StatusNotFound
			var errMessage string
			if errors.Is(err, errInvalidGeneration) {
				statusCode = http.StatusBadRequest
				errMessage = err.Error()
			}
			return jsonResponse{
				status:       statusCode,
				errorMessage: errMessage,
			}
		}
		header := make(http.Header)
		header.Set("Accept-Ranges", "bytes")
		return jsonResponse{
			header: header,
			data:   newProjectedObjectResponse(obj.ObjectAttrs, s.externalURL, projection),
		}
	})

	handler(w, r)
}

func (s *Server) deleteObject(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	obj, err := s.GetObjectStreaming(vars["bucketName"], vars["objectName"])
	// Calling Close before checking err is okay on objects, and the object
	// may need to be closed whether or not there's an error.
	defer obj.Close() //lint:ignore SA5001 // see above
	if err == nil {
		err = s.backend.DeleteObject(vars["bucketName"], vars["objectName"])
	}
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	bucket, _ := s.backend.GetBucket(obj.BucketName)
	backendObj := toBackendObjects([]StreamingObject{obj})[0]
	if bucket.VersioningEnabled {
		s.eventManager.Trigger(&backendObj, notification.EventArchive, nil)
	} else {
		s.eventManager.Trigger(&backendObj, notification.EventDelete, nil)
	}
	return jsonResponse{}
}

func (s *Server) listObjectACL(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))

	obj, err := s.GetObjectStreaming(vars["bucketName"], vars["objectName"])
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	defer obj.Close()

	return jsonResponse{data: newACLListResponse(obj.ObjectAttrs)}
}

func (s *Server) deleteObjectACL(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))

	obj, err := s.GetObjectStreaming(vars["bucketName"], vars["objectName"])
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	defer obj.Close()
	entity := vars["entity"]

	var newAcls []storage.ACLRule
	for _, aclRule := range obj.ObjectAttrs.ACL {
		if entity != string(aclRule.Entity) {
			newAcls = append(newAcls, aclRule)
		}
	}

	obj.ACL = newAcls
	obj, err = s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return errToJsonResponse(err)
	}
	defer obj.Close()

	return jsonResponse{status: http.StatusOK}
}

func (s *Server) getObjectACL(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))

	obj, err := s.backend.GetObject(vars["bucketName"], vars["objectName"])
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	defer obj.Close()
	entity := vars["entity"]

	for _, aclRule := range obj.ObjectAttrs.ACL {
		if entity == string(aclRule.Entity) {
			oac := &objectAccessControl{
				Bucket: obj.BucketName,
				Entity: string(aclRule.Entity),
				Object: obj.Name,
				Role:   string(aclRule.Role),
				Etag:   "RVRhZw==",
				Kind:   "storage#objectAccessControl",
			}
			return jsonResponse{data: oac}
		}
	}
	return jsonResponse{status: http.StatusNotFound}
}

func (s *Server) setObjectACL(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))

	obj, err := s.GetObjectStreaming(vars["bucketName"], vars["objectName"])
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	defer obj.Close()

	var data struct {
		Entity string
		Role   string
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: err.Error(),
		}
	}

	entity := storage.ACLEntity(data.Entity)
	role := storage.ACLRole(data.Role)
	obj.ACL = []storage.ACLRule{{
		Entity: entity,
		Role:   role,
	}}

	obj, err = s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return errToJsonResponse(err)
	}
	defer obj.Close()

	return jsonResponse{data: newACLListResponse(obj.ObjectAttrs)}
}

func (s *Server) rewriteObject(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	obj, err := s.objectWithGenerationOnValidGeneration(vars["sourceBucket"], vars["sourceObject"], r.FormValue("sourceGeneration"))
	// Calling Close before checking err is okay on objects, and the object
	// may need to be closed whether or not there's an error.
	defer obj.Close() //lint:ignore SA5001 // see above
	if err != nil {
		statusCode := http.StatusNotFound
		var errMessage string
		if errors.Is(err, errInvalidGeneration) {
			statusCode = http.StatusBadRequest
			errMessage = err.Error()
		}
		return jsonResponse{errorMessage: errMessage, status: statusCode}
	}

	var metadata multipartMetadata
	err = json.NewDecoder(r.Body).Decode(&metadata)
	if err != nil && err != io.EOF { // The body is optional
		return jsonResponse{errorMessage: "Invalid metadata", status: http.StatusBadRequest}
	}

	// Only supplied metadata overwrites the new object's metdata
	if len(metadata.Metadata) == 0 {
		metadata.Metadata = obj.Metadata
	}
	if metadata.ContentType == "" {
		metadata.ContentType = obj.ContentType
	}
	if metadata.ContentEncoding == "" {
		metadata.ContentEncoding = obj.ContentEncoding
	}
	if metadata.ContentDisposition == "" {
		metadata.ContentDisposition = obj.ContentDisposition
	}
	if metadata.ContentLanguage == "" {
		metadata.ContentLanguage = obj.ContentLanguage
	}

	dstBucket := vars["destinationBucket"]
	newObject := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:         dstBucket,
			Name:               vars["destinationObject"],
			ACL:                obj.ACL,
			ContentType:        metadata.ContentType,
			ContentEncoding:    metadata.ContentEncoding,
			ContentDisposition: metadata.ContentDisposition,
			ContentLanguage:    metadata.ContentLanguage,
			Metadata:           metadata.Metadata,
		},
		Content: obj.Content,
	}

	created, err := s.createObject(newObject, backend.NoConditions{})
	if err != nil {
		return errToJsonResponse(err)
	}
	defer created.Close()

	if vars["copyType"] == "copyTo" {
		return jsonResponse{data: newObjectResponse(created.ObjectAttrs, s.externalURL)}
	}
	return jsonResponse{data: newObjectRewriteResponse(created.ObjectAttrs, s.externalURL)}
}

func (s *Server) downloadObject(w http.ResponseWriter, r *http.Request) {
	vars := unescapeMuxVars(mux.Vars(r))
	obj, err := s.objectWithGenerationOnValidGeneration(vars["bucketName"], vars["objectName"], r.FormValue("generation"))
	// Calling Close before checking err is okay on objects, and the object
	// may need to be closed whether or not there's an error.
	defer obj.Close() //lint:ignore SA5001 // see above
	if err != nil {
		statusCode := http.StatusNotFound
		message := http.StatusText(statusCode)
		if errors.Is(err, errInvalidGeneration) {
			statusCode = http.StatusBadRequest
			message = err.Error()
		}
		http.Error(w, message, statusCode)
		return
	}

	var content io.Reader
	content = obj.Content
	status := http.StatusOK

	transcoded := false
	ranged := false
	start := int64(0)
	lastByte := int64(0)
	satisfiable := true
	contentLength := int64(0)

	handledTranscoding := func() bool {
		// This should also be false if the Cache-Control metadata field == "no-transform",
		// but we don't currently support that field.
		// See https://cloud.google.com/storage/docs/transcoding

		if obj.ContentEncoding == "gzip" && !strings.Contains(r.Header.Get("accept-encoding"), "gzip") {
			// GCS will transparently decompress gzipped content, see
			// https://cloud.google.com/storage/docs/transcoding
			// In this case, any Range header is ignored and the full content is returned.

			// If the content is not a valid gzip file, ignore errors and continue
			// without transcoding. Otherwise, return decompressed content.
			gzipReader, err := gzip.NewReader(content)
			if err == nil {
				rawContent, err := io.ReadAll(gzipReader)
				if err == nil {
					transcoded = true
					content = bytes.NewReader(rawContent)
					contentLength = int64(len(rawContent))
					obj.Size = contentLength
					return true
				}
			}
		}
		return false
	}

	if !handledTranscoding() {
		ranged, start, lastByte, satisfiable = s.handleRange(obj, r)
		contentLength = lastByte - start + 1
	}

	if ranged && satisfiable {
		_, err = obj.Content.Seek(start, io.SeekStart)
		if err != nil {
			http.Error(w, "could not seek", http.StatusInternalServerError)
			return
		}
		content = io.LimitReader(obj.Content, contentLength)
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, lastByte, obj.Size))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("X-Goog-Generation", strconv.FormatInt(obj.Generation, 10))
	w.Header().Set("X-Goog-Hash", fmt.Sprintf("crc32c=%s,md5=%s", obj.Crc32c, obj.Md5Hash))
	w.Header().Set("Last-Modified", obj.Updated.Format(http.TimeFormat))
	w.Header().Set("ETag", fmt.Sprintf("%q", obj.Etag))
	for name, value := range obj.Metadata {
		w.Header().Set("X-Goog-Meta-"+name, value)
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if ranged && !satisfiable {
		status = http.StatusRequestedRangeNotSatisfiable
		content = bytes.NewReader([]byte(fmt.Sprintf(`<?xml version='1.0' encoding='UTF-8'?>`+
			`<Error><Code>InvalidRange</Code>`+
			`<Message>The requested range cannot be satisfied.</Message>`+
			`<Details>%s</Details></Error>`, r.Header.Get("Range"))))
		w.Header().Set(contentTypeHeader, "application/xml; charset=UTF-8")
	} else {
		if obj.ContentType != "" {
			w.Header().Set(contentTypeHeader, obj.ContentType)
		}
		if obj.CacheControl != "" {
			w.Header().Set(cacheControlHeader, obj.CacheControl)
		}
		// If content was transcoded, the underlying encoding was removed so we shouldn't report it.
		if obj.ContentEncoding != "" && !transcoded {
			w.Header().Set("Content-Encoding", obj.ContentEncoding)
		}
		if obj.ContentDisposition != "" {
			w.Header().Set("Content-Disposition", obj.ContentDisposition)
		}
		if obj.ContentLanguage != "" {
			w.Header().Set("Content-Language", obj.ContentLanguage)
		}
		// X-Goog-Stored-Content-Encoding must be set to the original encoding,
		// defaulting to "identity" if no encoding was set.
		storedContentEncoding := "identity"
		if obj.ContentEncoding != "" {
			storedContentEncoding = obj.ContentEncoding
		}
		w.Header().Set("X-Goog-Stored-Content-Encoding", storedContentEncoding)
	}

	w.WriteHeader(status)
	if r.Method == http.MethodGet {
		io.Copy(w, content)
	}
}

func (s *Server) handleRange(obj StreamingObject, r *http.Request) (ranged bool, start int64, lastByte int64, satisfiable bool) {
	start, end, err := parseRange(r.Header.Get("Range"), obj.Size)
	if err != nil {
		// If the range isn't valid, GCS returns all content.
		return false, 0, obj.Size - 1, false
	}
	// GCS is pretty flexible when it comes to invalid ranges. A 416 http
	// response is only returned when the range start is beyond the length of
	// the content. Otherwise, the range is ignored.
	switch {
	// Invalid start. Return 416 and NO content.
	// Examples:
	//   Length: 40, Range: bytes=50-60
	//   Length: 40, Range: bytes=50-
	case start >= obj.Size:
		// This IS a ranged request, but it ISN'T satisfiable.
		return true, 0, -1, false
	// Negative range, ignore range and return all content.
	// Examples:
	//   Length: 40, Range: bytes=30-20
	case end < start:
		return false, 0, obj.Size - 1, false
	// Return range. Clamp start and end.
	// Examples:
	//   Length: 40, Range: bytes=-100
	//   Length: 40, Range: bytes=0-100
	default:
		if start < 0 {
			start = 0
		}
		if end >= obj.Size {
			end = obj.Size - 1
		}
		return true, start, end, true
	}
}

// parseRange parses the range header and returns the corresponding start and
// end indices in the content. The end index is inclusive. This function
// doesn't validate that the start and end indices fall within the content
// bounds. The content length is only used to handle "suffix length" and
// range-to-end ranges.
func parseRange(rangeHeaderValue string, contentLength int64) (start int64, end int64, err error) {
	// For information about the range header, see:
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Range
	// https://httpwg.org/specs/rfc7233.html#header.range
	// https://httpwg.org/specs/rfc7233.html#byte.ranges
	// https://httpwg.org/specs/rfc7233.html#status.416
	//
	// <unit>=<range spec>
	//
	// The following ranges are parsed:
	// "bytes=40-50" (range with given start and end)
	// "bytes=40-"   (range to end of content)
	// "bytes=-40"   (suffix length, offset from end of string)
	//
	// The unit MUST be "bytes".
	parts := strings.SplitN(rangeHeaderValue, "=", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expecting `=` in range header, got: %s", rangeHeaderValue)
	}
	if parts[0] != "bytes" {
		return 0, 0, fmt.Errorf("invalid range unit, expecting `bytes`, got: %s", parts[0])
	}
	rangeSpec := parts[1]
	if len(rangeSpec) == 0 {
		return 0, 0, errors.New("empty range")
	}
	if rangeSpec[0] == '-' {
		offsetFromEnd, err := strconv.ParseInt(rangeSpec, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid suffix length, got: %s", rangeSpec)
		}
		start = contentLength + offsetFromEnd
		end = contentLength - 1
	} else {
		rangeParts := strings.SplitN(rangeSpec, "-", 2)
		if len(rangeParts) != 2 {
			return 0, 0, fmt.Errorf("only one range supported, got: %s", rangeSpec)
		}
		start, err = strconv.ParseInt(rangeParts[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start, got: %s", rangeParts[0])
		}
		if rangeParts[1] == "" {
			end = contentLength - 1
		} else {
			end, err = strconv.ParseInt(rangeParts[1], 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid range end, got: %s", rangeParts[1])
			}
		}
	}
	return start, end, nil
}

func (s *Server) patchObject(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	bucketName := vars["bucketName"]
	objectName := vars["objectName"]

	type acls struct {
		Entity string
		Role   string
	}

	var payload struct {
		ContentType        string
		ContentEncoding    string
		ContentDisposition string
		ContentLanguage    string
		Metadata           map[string]string `json:"metadata"`
		CustomTime         string
		Acl                []acls
	}
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "Metadata in the request couldn't decode",
		}
	}

	var attrsToUpdate backend.ObjectAttrs

	attrsToUpdate.ContentType = payload.ContentType
	attrsToUpdate.ContentEncoding = payload.ContentEncoding
	attrsToUpdate.ContentDisposition = payload.ContentDisposition
	attrsToUpdate.ContentLanguage = payload.ContentLanguage
	attrsToUpdate.Metadata = payload.Metadata
	attrsToUpdate.CustomTime = payload.CustomTime

	if len(payload.Acl) > 0 {
		attrsToUpdate.ACL = []storage.ACLRule{}
		for _, aclData := range payload.Acl {
			newAcl := storage.ACLRule{Entity: storage.ACLEntity(aclData.Entity), Role: storage.ACLRole(aclData.Role)}
			attrsToUpdate.ACL = append(attrsToUpdate.ACL, newAcl)
		}
	}

	backendObj, err := s.backend.PatchObject(bucketName, objectName, attrsToUpdate)
	if err != nil {
		return jsonResponse{
			status:       http.StatusNotFound,
			errorMessage: "Object not found to be PATCHed",
		}
	}
	defer backendObj.Close()

	s.eventManager.Trigger(&backendObj, notification.EventMetadata, nil)
	return jsonResponse{data: fromBackendObjects([]backend.StreamingObject{backendObj})[0]}
}

func (s *Server) updateObject(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	bucketName := vars["bucketName"]
	objectName := vars["objectName"]

	type acls struct {
		Entity string
		Role   string
	}

	var payload struct {
		Metadata           map[string]string `json:"metadata"`
		ContentType        string            `json:"contentType"`
		ContentDisposition string            `json:"contentDisposition"`
		ContentLanguage    string            `json:"contentLanguage"`
		CustomTime         string
		Acl                []acls
	}
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "Metadata in the request couldn't decode",
		}
	}

	var attrsToUpdate backend.ObjectAttrs

	attrsToUpdate.Metadata = payload.Metadata
	attrsToUpdate.CustomTime = payload.CustomTime
	attrsToUpdate.ContentType = payload.ContentType
	attrsToUpdate.ContentDisposition = payload.ContentDisposition
	attrsToUpdate.ContentLanguage = payload.ContentLanguage
	if len(payload.Acl) > 0 {
		attrsToUpdate.ACL = []storage.ACLRule{}
		for _, aclData := range payload.Acl {
			newAcl := storage.ACLRule{Entity: storage.ACLEntity(aclData.Entity), Role: storage.ACLRole(aclData.Role)}
			attrsToUpdate.ACL = append(attrsToUpdate.ACL, newAcl)
		}
	}
	backendObj, err := s.backend.UpdateObject(bucketName, objectName, attrsToUpdate)
	if err != nil {
		return jsonResponse{
			status:       http.StatusNotFound,
			errorMessage: "Object not found to be updated",
		}
	}
	defer backendObj.Close()

	s.eventManager.Trigger(&backendObj, notification.EventMetadata, nil)
	return jsonResponse{data: fromBackendObjects([]backend.StreamingObject{backendObj})[0]}
}

func (s *Server) composeObject(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	bucketName := vars["bucketName"]
	destinationObject := vars["destinationObject"]

	var composeRequest struct {
		SourceObjects []struct {
			Name string
		}
		Destination struct {
			Bucket             string
			ContentType        string
			ContentDisposition string
			ContentLanguage    string
			Metadata           map[string]string
		}
	}

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&composeRequest)
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "Error parsing request body",
		}
	}

	const maxComposeObjects = 32
	if len(composeRequest.SourceObjects) > maxComposeObjects {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: fmt.Sprintf("The number of source components provided (%d) exceeds the maximum (%d)", len(composeRequest.SourceObjects), maxComposeObjects),
		}
	}

	sourceNames := make([]string, 0, len(composeRequest.SourceObjects))
	for _, n := range composeRequest.SourceObjects {
		sourceNames = append(sourceNames, n.Name)
	}

	backendObj, err := s.backend.ComposeObject(bucketName, sourceNames, destinationObject, composeRequest.Destination.Metadata, composeRequest.Destination.ContentType, composeRequest.Destination.ContentDisposition, composeRequest.Destination.ContentLanguage)
	if err != nil {
		return jsonResponse{
			status:       http.StatusInternalServerError,
			errorMessage: "Error running compose",
		}
	}
	defer backendObj.Close()

	obj := fromBackendObjects([]backend.StreamingObject{backendObj})[0]

	s.eventManager.Trigger(&backendObj, notification.EventFinalize, nil)

	return jsonResponse{data: newObjectResponse(obj.ObjectAttrs, s.externalURL)}
}
