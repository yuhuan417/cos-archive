package main

import (
	"bytes"
	"encoding/xml"
	"hash/crc64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStoredObject struct {
	body   []byte
	header http.Header
}

type fakeResponseSpec struct {
	status  int
	code    string
	message string
	body    string
	headers http.Header
}

type fakeListBucketResult struct {
	XMLName     xml.Name         `xml:"ListBucketResult"`
	Prefix      string           `xml:"Prefix,omitempty"`
	Marker      string           `xml:"Marker,omitempty"`
	NextMarker  string           `xml:"NextMarker,omitempty"`
	MaxKeys     int              `xml:"MaxKeys"`
	IsTruncated bool             `xml:"IsTruncated"`
	Contents    []fakeListObject `xml:"Contents,omitempty"`
}

type fakeListObject struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

type fakeErrorBody struct {
	XMLName xml.Name `xml:"Error"`
	Code    string
	Message string
}

type fakeCOSServer struct {
	t              *testing.T
	srv            *httptest.Server
	mu             sync.Mutex
	objects        map[string]*fakeStoredObject
	failures       map[string][]fakeResponseSpec
	stickyFailures map[string]fakeResponseSpec
	listPageSize   int
	requests       []string
}

func newFakeCOSServer(t *testing.T) *fakeCOSServer {
	t.Helper()
	f := &fakeCOSServer{
		t:              t,
		objects:        make(map[string]*fakeStoredObject),
		failures:       make(map[string][]fakeResponseSpec),
		stickyFailures: make(map[string]fakeResponseSpec),
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCOSServer) newClient(t *testing.T, config COSConfig) *COS {
	t.Helper()
	if config.URL == "" {
		config.URL = f.srv.URL
	}
	c, err := NewCOS(config)
	if err != nil {
		t.Fatalf("NewCOS: %v", err)
	}
	return c
}

func (f *fakeCOSServer) setListPageSize(size int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listPageSize = size
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return make(http.Header)
	}
	out := make(http.Header, len(h))
	for k, values := range h {
		out[k] = append([]string(nil), values...)
	}
	return out
}

func (f *fakeCOSServer) setObject(key string, body []byte, header http.Header) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dupHeader := cloneHeader(header)
	if dupHeader.Get("Last-Modified") == "" {
		dupHeader.Set("Last-Modified", time.Now().UTC().Format("Mon, 2 Jan 2006 15:04:05 MST"))
	}
	f.objects[key] = &fakeStoredObject{
		body:   append([]byte(nil), body...),
		header: dupHeader,
	}
}

func (f *fakeCOSServer) addFailure(method string, key string, specs ...fakeResponseSpec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := method + " " + key
	f.failures[id] = append(f.failures[id], specs...)
}

// setPersistentFailure makes every matching request fail for the rest of the
// test. The SDK retries 5xx responses on its own, so a test asserting that an
// error surfaces must keep failing for every attempt.
func (f *fakeCOSServer) setPersistentFailure(method string, key string, spec fakeResponseSpec) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stickyFailures[method+" "+key] = spec
}

func (f *fakeCOSServer) objectExists(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

func (f *fakeCOSServer) objectBody(key string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := f.objects[key]
	if obj == nil {
		return nil
	}
	return append([]byte(nil), obj.body...)
}

func (f *fakeCOSServer) objectHeader(key string) http.Header {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := f.objects[key]
	if obj == nil {
		return nil
	}
	return cloneHeader(obj.header)
}

func (f *fakeCOSServer) objectKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (f *fakeCOSServer) requestCount(method string, key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	target := method + " " + key
	count := 0
	for _, req := range f.requests {
		if req == target {
			count++
		}
	}
	return count
}

func (f *fakeCOSServer) popFailure(method string, key string) *fakeResponseSpec {
	id := method + " " + key
	if spec, ok := f.stickyFailures[id]; ok {
		return &spec
	}
	queue := f.failures[id]
	if len(queue) == 0 {
		return nil
	}
	next := queue[0]
	f.failures[id] = queue[1:]
	return &next
}

func (f *fakeCOSServer) handle(w http.ResponseWriter, r *http.Request) {
	key, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	rawKey := key
	if r.URL.RawQuery != "" {
		rawKey += "?" + r.URL.RawQuery
	}

	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+rawKey)
	spec := f.popFailure(r.Method, rawKey)
	f.mu.Unlock()
	if spec != nil {
		f.writeFailure(w, spec)
		return
	}

	if r.Method == http.MethodGet && key == "" {
		f.handleList(w, r)
		return
	}

	switch r.Method {
	case http.MethodHead:
		f.handleHead(w, key)
	case http.MethodGet:
		f.handleGet(w, key)
	case http.MethodPut:
		f.handlePut(w, r, key)
	case http.MethodDelete:
		f.handleDelete(w, key)
	case http.MethodPost:
		if _, ok := r.URL.Query()["restore"]; ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unsupported post", http.StatusBadRequest)
	default:
		http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
	}
}

func (f *fakeCOSServer) writeFailure(w http.ResponseWriter, spec *fakeResponseSpec) {
	for k, values := range cloneHeader(spec.headers) {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	status := spec.status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
	if spec.body != "" {
		_, _ = io.WriteString(w, spec.body)
		return
	}
	if spec.code != "" {
		_ = xml.NewEncoder(w).Encode(fakeErrorBody{
			Code:    spec.code,
			Message: spec.message,
		})
	}
}

func (f *fakeCOSServer) handleHead(w http.ResponseWriter, key string) {
	f.mu.Lock()
	obj := f.objects[key]
	f.mu.Unlock()
	if obj == nil {
		f.writeFailure(w, &fakeResponseSpec{status: http.StatusNotFound, code: "NoSuchKey", message: "missing"})
		return
	}
	for k, values := range cloneHeader(obj.header) {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
	w.WriteHeader(http.StatusOK)
}

func (f *fakeCOSServer) handleGet(w http.ResponseWriter, key string) {
	f.mu.Lock()
	obj := f.objects[key]
	f.mu.Unlock()
	if obj == nil {
		f.writeFailure(w, &fakeResponseSpec{status: http.StatusNotFound, code: "NoSuchKey", message: "missing"})
		return
	}
	for k, values := range cloneHeader(obj.header) {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
	setCRC64Header(w, obj.body)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(obj.body)
}

// setCRC64Header mirrors the x-cos-hash-crc64ecma header real COS returns;
// the SDK verifies downloads against it.
func setCRC64Header(w http.ResponseWriter, body []byte) {
	crc := crc64.Checksum(body, crc64.MakeTable(crc64.ECMA))
	w.Header().Set("x-cos-hash-crc64ecma", strconv.FormatUint(crc, 10))
}

func (f *fakeCOSServer) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	header := make(http.Header)
	for k, values := range r.Header {
		header[k] = append([]string(nil), values...)
	}
	if header.Get("Last-Modified") == "" {
		header.Set("Last-Modified", time.Now().UTC().Format("Mon, 2 Jan 2006 15:04:05 MST"))
	}
	f.setObject(key, body, header)
	w.Header().Set("ETag", "fake-etag")
	setCRC64Header(w, body)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeCOSServer) handleDelete(w http.ResponseWriter, key string) {
	f.mu.Lock()
	delete(f.objects, key)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeCOSServer) handleList(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	marker := r.URL.Query().Get("marker")
	maxKeys := 1000
	if v := r.URL.Query().Get("max-keys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxKeys = n
		}
	}

	f.mu.Lock()
	pageSize := f.listPageSize
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	f.mu.Unlock()

	if pageSize > 0 {
		maxKeys = pageSize
	}

	start := 0
	if marker != "" {
		for i, key := range keys {
			if key > marker {
				start = i
				break
			}
			if i == len(keys)-1 {
				start = len(keys)
			}
		}
	}

	end := start + maxKeys
	if end > len(keys) {
		end = len(keys)
	}
	page := keys[start:end]
	result := fakeListBucketResult{
		Prefix:      prefix,
		Marker:      marker,
		MaxKeys:     maxKeys,
		IsTruncated: end < len(keys),
		Contents:    make([]fakeListObject, 0, len(page)),
	}
	if result.IsTruncated && len(page) > 0 {
		result.NextMarker = page[len(page)-1]
	}

	for _, key := range page {
		f.mu.Lock()
		obj := f.objects[key]
		f.mu.Unlock()
		result.Contents = append(result.Contents, fakeListObject{
			Key:          key,
			Size:         int64(len(obj.body)),
			LastModified: obj.header.Get("Last-Modified"),
		})
	}

	w.Header().Set("Content-Type", "application/xml")
	if err := xml.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func mustMarshalIndex(t *testing.T, index *Index) []byte {
	t.Helper()
	fp := filepath.Join(t.TempDir(), "index.json")
	if err := index.Save(fp); err != nil {
		t.Fatalf("save index: %v", err)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	return b
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return buf.String()
}
