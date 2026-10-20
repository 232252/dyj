package ipp

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/232252/dyj/pkg/attributes"
)

// ---------------------------------------------------------------------------
// a JobSink fake that records everything the server does to it
// ---------------------------------------------------------------------------

type fakeSink struct {
	mu sync.Mutex

	beginErr  error
	addErr    error
	cancelErr error

	jobs    map[int32]*fakeJob
	nextID  int32
	begun   []JobInfo
	added   []fakeAdd
	cancels []int32
}

type fakeJob struct {
	view JobView
}

type fakeAdd struct {
	jobID int32
	doc   Document
	last  bool
	data  []byte
}

func newFakeSink() *fakeSink {
	return &fakeSink{jobs: map[int32]*fakeJob{}, nextID: 100}
}

func (f *fakeSink) Begin(info JobInfo) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beginErr != nil {
		return 0, f.beginErr
	}
	f.nextID++
	f.begun = append(f.begun, info)
	f.jobs[f.nextID] = &fakeJob{view: JobView{
		ID: f.nextID, State: JobStateHeld, Name: info.Name, User: info.UserName,
		Format: "application/octet-stream",
	}}
	return f.nextID, nil
}

func (f *fakeSink) AddDocument(id int32, doc Document, last bool) error {
	data, err := io.ReadAll(doc.Data)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.jobs[id]; !ok {
		return ErrUnknownJob
	}
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, fakeAdd{jobID: id, doc: doc, last: last, data: data})
	if last {
		f.jobs[id].view.State = JobStatePending
	}
	return nil
}

func (f *fakeSink) Cancel(id int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	if _, ok := f.jobs[id]; !ok {
		return ErrUnknownJob
	}
	if f.cancelErr != nil {
		return f.cancelErr
	}
	switch f.jobs[id].view.State {
	case JobStatePending, JobStateHeld, JobStateProcessing:
		f.jobs[id].view.State = JobStateCanceled
		return nil
	default:
		return ErrJobFinished
	}
}

func (f *fakeSink) Snapshot(which string) []JobView {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []JobView
	for _, j := range f.jobs {
		v := j.view
		switch which {
		case "completed":
			if v.State != JobStateCompleted && v.State != JobStateCanceled && v.State != JobStateAborted {
				continue
			}
		case "not-completed", "":
			if v.State == JobStateCompleted || v.State == JobStateCanceled || v.State == JobStateAborted {
				continue
			}
		}
		out = append(out, v)
	}
	return out
}

func (f *fakeSink) View(id int32) (JobView, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return JobView{}, false
	}
	return j.view, true
}

func (f *fakeSink) Status() (bool, int32, []string) {
	return true, int32(len(f.jobs)), nil
}

// ---------------------------------------------------------------------------
// request builders
// ---------------------------------------------------------------------------

// buildRequest writes an IPP request with an operation group, optional
// job group, and optional document bytes.
type reqOpt func(*requestBuilder)

type requestBuilder struct {
	opAttrs  [][2]string // name,value; repeated name = 1setOf
	opInts   [][2]any    // name, int32 value in the OPERATION group
	opBools  [][2]any    // name, boolean value in the OPERATION group
	jobAttrs [][2]string
	jobInts  [][2]any // name, int32 value
	data     []byte
}

func opAttr(kv ...string) reqOpt       { return func(b *requestBuilder) { b.opAttrs = append(b.opAttrs, [2]string{kv[0], kv[1]}) } }
func jobAttr(kv ...string) reqOpt      { return func(b *requestBuilder) { b.jobAttrs = append(b.jobAttrs, [2]string{kv[0], kv[1]}) } }
func jobInt(name string, v int32) reqOpt {
	return func(b *requestBuilder) { b.jobInts = append(b.jobInts, [2]any{name, v}) }
}
func opInt(name string, v int32) reqOpt {
	return func(b *requestBuilder) { b.opInts = append(b.opInts, [2]any{name, v}) }
}
func opBool(name string, v bool) reqOpt {
	return func(b *requestBuilder) { b.opBools = append(b.opBools, [2]any{name, v}) }
}
func jobData(d []byte) reqOpt { return func(b *requestBuilder) { b.data = d } }

func buildRequest(t *testing.T, op uint16, id int32, opts ...reqOpt) []byte {
	t.Helper()
	b := &requestBuilder{}
	for _, o := range opts {
		o(b)
	}
	var out bytes.Buffer
	out.Write([]byte{2, 0, byte(op >> 8), byte(op)})
	_ = binary.Write(&out, binary.BigEndian, id)
	out.WriteByte(attributes.TagOperationAttributes)
	writeKV(t, &out, attributes.TagCharset, "attributes-charset", "utf-8")
	writeKV(t, &out, attributes.TagNaturalLang, "attributes-natural-language", "en-us")
	writeKV(t, &out, attributes.TagURI, "printer-uri", "ipp://127.0.0.1/ipp/print")
	for _, kv := range b.opAttrs {
		writeKV(t, &out, attributes.TagKeyword, kv[0], kv[1])
	}
	for _, kv := range b.opInts {
		name := kv[0].(string)
		out.WriteByte(attributes.TagInteger)
		writeLen(&out, name)
		out.WriteString(name)
		writeLen(&out, string([]byte{0, 0, 0, 0}))
		_ = binary.Write(&out, binary.BigEndian, kv[1].(int32))
	}
	for _, kv := range b.opBools {
		name := kv[0].(string)
		out.WriteByte(attributes.TagBoolean)
		writeLen(&out, name)
		out.WriteString(name)
		writeLen(&out, string([]byte{0}))
		v := byte(0)
		if kv[1].(bool) {
			v = 1
		}
		out.WriteByte(v)
	}
	if len(b.jobAttrs) > 0 || len(b.jobInts) > 0 {
		out.WriteByte(attributes.TagJobAttributes)
		for _, kv := range b.jobAttrs {
			writeKV(t, &out, attributes.TagKeyword, kv[0], kv[1])
		}
		for _, kv := range b.jobInts {
			name := kv[0].(string)
			out.WriteByte(attributes.TagInteger)
			writeLen(&out, name)
			out.WriteString(name)
			writeLen(&out, string([]byte{0, 0, 0, 0}))
			_ = binary.Write(&out, binary.BigEndian, kv[1].(int32))
		}
	}
	out.WriteByte(attributes.TagEndOfAttributes)
	out.Write(b.data)
	return out.Bytes()
}

func writeLen(b *bytes.Buffer, s string) {
	_ = binary.Write(b, binary.BigEndian, uint16(len(s)))
}

func writeKV(t *testing.T, b *bytes.Buffer, tag byte, name, value string) {
	t.Helper()
	b.WriteByte(tag)
	writeLen(b, name)
	b.WriteString(name)
	writeLen(b, value)
	b.WriteString(value)
}

// post submits a request to the test server and decodes the response.
func post(t *testing.T, ts *httptest.Server, raw []byte) (*attributes.Message, []byte) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != ContentTypeIPP {
		t.Fatalf("Content-Type = %q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := attributes.DecodeMessage(body)
	if err != nil {
		t.Fatalf("response not decodable: %v\nhex: %x", err, body)
	}
	return msg, body
}

func statusOf(t *testing.T, msg *attributes.Message) int16 {
	t.Helper()
	return int16(msg.Code)
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestPrintJobEndToEnd(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	doc := []byte("PWG-RASTER-BYTES-0123456789")
	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 7,
		opAttr("requesting-user-name", "alice"),
		opAttr("job-name", "hello world"),
		opAttr("document-format", "image/pwg-raster"),
		jobData(doc),
	))

	if statusOf(t, msg) != StatusOK {
		t.Fatalf("status = 0x%04X, want OK", msg.Code)
	}
	job := msg.Job()
	if job == nil {
		t.Fatal("no job group in Print-Job response")
	}
	id, ok := attributes.GroupInt(job, "job-id")
	if !ok || id != 101 {
		t.Fatalf("job-id = %d (%v), want 101", id, ok)
	}
	if state, _ := attributes.GroupInt(job, "job-state"); state != JobStatePending {
		t.Errorf("job-state = %d, want pending(3)", state)
	}
	if uri := attributes.GroupFirstString(job, "job-uri"); !strings.HasSuffix(uri, "/101") {
		t.Errorf("job-uri = %q, want suffix /101", uri)
	}

	if len(sink.begun) != 1 || sink.begun[0].UserName != "alice" || sink.begun[0].Name != "hello world" {
		t.Fatalf("Begin info = %+v", sink.begun)
	}
	if len(sink.added) != 1 {
		t.Fatalf("AddDocument calls = %d, want 1", len(sink.added))
	}
	a := sink.added[0]
	if !a.last || a.doc.Format != "image/pwg-raster" || string(a.data) != string(doc) {
		t.Fatalf("AddDocument = last=%v format=%q data=%q", a.last, a.doc.Format, a.data)
	}
}

func TestPrintJobGzipDocument(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	var zbuf bytes.Buffer
	zw := gzip.NewWriter(&zbuf)
	zw.Write([]byte("COMPRESSED-PWG-PAGE-DATA"))
	zw.Close()

	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 8,
		opAttr("compression", "gzip"),
		opAttr("document-format", "image/pwg-raster"),
		jobData(zbuf.Bytes()),
	))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("status = 0x%04X, want OK", msg.Code)
	}
	if len(sink.added) != 1 {
		t.Fatalf("AddDocument calls = %d", len(sink.added))
	}
	if got := string(sink.added[0].data); got != "COMPRESSED-PWG-PAGE-DATA" {
		t.Fatalf("decompressed data = %q", got)
	}
	if sink.added[0].doc.Compressed != "gzip" {
		t.Fatalf("Compressed = %q, want gzip", sink.added[0].doc.Compressed)
	}
}

func TestPrintJobUnknownCompressionRejected(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 9,
		opAttr("compression", "brotli"),
		jobData([]byte("x")),
	))
	// An undecodable compression is fatal regardless of fidelity:
	// continuing would accept bytes we must corrupt or drop.
	if statusOf(t, msg) != StatusErrorAttributesNotSupported {
		t.Fatalf("status = 0x%04X, want 0x040B", msg.Code)
	}
}

func TestMultiDocumentJob(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpCreateJob, 10, opAttr("job-name", "multi")))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("Create-Job status = 0x%04X", msg.Code)
	}
	id, _ := attributes.GroupInt(msg.Job(), "job-id")
	if state, _ := attributes.GroupInt(msg.Job(), "job-state"); state != JobStateHeld {
		t.Fatalf("Create-Job job-state = %d, want held(4)", state)
	}

	msg, _ = post(t, ts, buildRequest(t, OpSendDocument, 11,
		opInt("job-id", id),
		opBool("last-document", false),
		jobData([]byte("doc-one")),
	))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("Send-Document #1 status = 0x%04X", msg.Code)
	}

	msg, _ = post(t, ts, buildRequest(t, OpSendDocument, 12,
		opInt("job-id", id),
		opBool("last-document", true),
		opAttr("document-format", "image/pwg-raster"),
		jobData([]byte("doc-two")),
	))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("Send-Document #2 status = 0x%04X", msg.Code)
	}
	if state, _ := attributes.GroupInt(msg.Job(), "job-state"); state != JobStatePending {
		t.Fatalf("state after last-document = %d, want pending(3)", state)
	}
	if len(sink.added) != 2 || string(sink.added[1].data) != "doc-two" {
		t.Fatalf("added = %+v", sink.added)
	}
}

func TestSendDocumentUnknownJob(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpSendDocument, 13,
		opInt("job-id", 404),
		opBool("last-document", true),
		jobData([]byte("x")),
	))
	if statusOf(t, msg) != StatusErrorNotFound {
		t.Fatalf("status = 0x%04X, want 0x0406 not-found", msg.Code)
	}
}

func TestGetJobsAndCancel(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	// Two jobs.
	post(t, ts, buildRequest(t, OpPrintJob, 20, opAttr("job-name", "j1"), jobData([]byte("a"))))
	post(t, ts, buildRequest(t, OpPrintJob, 21, opAttr("job-name", "j2"), jobData([]byte("b"))))

	msg, _ := post(t, ts, buildRequest(t, OpGetJobs, 22, opAttr("which-jobs", "not-completed")))
	jobGroups := 0
	for i := range msg.Groups {
		if msg.Groups[i].Delimiter == GroupJob {
			jobGroups++
		}
	}
	if jobGroups != 2 {
		t.Fatalf("Get-Jobs returned %d job groups, want 2", jobGroups)
	}

	// Cancel the first one and ask again.
	msg, _ = post(t, ts, buildRequest(t, OpCancelJob, 23, opInt("job-id", 101)))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("Cancel status = 0x%04X", msg.Code)
	}
	if len(sink.cancels) != 1 || sink.cancels[0] != 101 {
		t.Fatalf("cancels = %v", sink.cancels)
	}

	msg, _ = post(t, ts, buildRequest(t, OpGetJobAttributes, 24, opInt("job-id", 101)))
	if state, _ := attributes.GroupInt(msg.Job(), "job-state"); state != JobStateCanceled {
		t.Fatalf("state after cancel = %d, want canceled(7)", state)
	}

	// Canceling again is client-error-not-possible (0x0503).
	msg, _ = post(t, ts, buildRequest(t, OpCancelJob, 25, opInt("job-id", 101)))
	if statusOf(t, msg) != StatusErrorNotPossible {
		t.Fatalf("second cancel status = 0x%04X, want 0x0503", msg.Code)
	}
}

func TestValidateJobRejectsUnknownFormat(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpValidateJob, 30,
		opAttr("document-format", "application/x- alien")))
	// fidelity defaults to false → Validate-Job reports the unsupported
	// attribute but does not hard-fail? RFC 8011 §4.2.2: Validate-Job
	// returns the same rejection the Print-Job would give. With fidelity
	// unset, unknown formats are still rejected by Print-Job (we cannot
	// render them at all), so Validate-Job must reject too.
	if statusOf(t, msg) == StatusOK {
		t.Fatalf("Validate-Job accepted an unrenderable format")
	}
}

func TestFidelityRejectsUnsupportedTemplate(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	raw := buildRequest(t, OpPrintJob, 31,
		opBool("ipp-attribute-fidelity", true),
		jobAttr("media", "iso_a0_841x1189mm"), // not in media-supported
		jobInt("copies", 500),                 // outside copies-supported
		jobData([]byte("x")),
	)
	resp, err := http.Post(ts.URL+"/ipp/print", ContentTypeIPP, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if status := binary.BigEndian.Uint16(body[2:4]); status != 0x040B {
		t.Fatalf("status = 0x%04X, want 0x040B attributes-or-values-not-supported", status)
	}
	msg, err := attributes.DecodeMessage(body)
	if err != nil {
		t.Fatal(err)
	}
	unsup := msg.Group(GroupUnsupported)
	if unsup == nil {
		t.Fatal("no unsupported-attributes group in 0x040B response")
	}
	names := map[string]bool{}
	for _, a := range unsup.Attributes {
		names[a.Name] = true
		if a.Tag != TagUnsupported {
			t.Errorf("unsupported attr %q tag = 0x%02X, want out-of-band 0x10", a.Name, a.Tag)
		}
	}
	if !names["media"] || !names["copies"] {
		t.Fatalf("unsupported group = %v, want media+copies", names)
	}
	if len(sink.begun) != 0 {
		t.Fatalf("job must not be created on fidelity rejection; begun=%d", len(sink.begun))
	}
}

func TestNoFidelityDropsBadTemplate(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 32,
		jobAttr("media", "iso_a0_841x1189mm"),
		jobInt("copies", 500),
		jobData([]byte("x")),
	))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("status = 0x%04X; without fidelity the job must proceed", msg.Code)
	}
}

func TestIdentifyPrinterSucceeds(t *testing.T) {
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpIdentifyPrinter, 33))
	if statusOf(t, msg) != StatusOK {
		t.Fatalf("Identify-Printer status = 0x%04X, want OK", msg.Code)
	}
}

func TestJobOpsWithoutSinkAreHonest(t *testing.T) {
	srv := NewServer(Options{Printer: DefaultPrinter()})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 40, jobData([]byte("x"))))
	if statusOf(t, msg) != 0x0506 {
		t.Fatalf("Print-Job without sink status = 0x%04X, want 0x0506", msg.Code)
	}
}

func TestSinkErrorIsReported(t *testing.T) {
	sink := newFakeSink()
	sink.addErr = errors.New("disk full")
	srv := NewServer(Options{Printer: DefaultPrinter(), Sink: sink})
	ts := httptest.NewServer(http.HandlerFunc(srv.handle))
	defer ts.Close()

	msg, _ := post(t, ts, buildRequest(t, OpPrintJob, 41, jobData([]byte("x"))))
	if statusOf(t, msg) == StatusOK {
		t.Fatal("sink error must surface to the client")
	}
}

var _ = context.Background
var _ = fmt.Sprintf
