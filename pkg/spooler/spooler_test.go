package spooler

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sq199/dyj/pkg/driver"
	"github.com/sq199/dyj/pkg/ipp"
)

func newGzipWriter(w io.Writer) *gzip.Writer { return gzip.NewWriter(w) }

// recordingBackend records Print calls and can block/fail on command.
type recordingBackend struct {
	mu       sync.Mutex
	printed  []driver.PrintJob
	failNext bool
	blockAll bool
}

func (b *recordingBackend) Name() string { return "recording" }

func (b *recordingBackend) Print(ctx context.Context, job driver.PrintJob, doc driver.Document) error {
	b.mu.Lock()
	b.printed = append(b.printed, job)
	fail := b.failNext
	b.mu.Unlock()

	if b.blockAll {
		<-ctx.Done()
		return ctx.Err()
	}
	if fail {
		return errors.New("simulated driver failure")
	}
	// Drain the document so the spool file read succeeds.
	rc, err := doc.Open()
	if err == nil {
		_, _ = io.Copy(io.Discard, rc)
		rc.Close()
	}
	return nil
}

func (b *recordingBackend) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.printed)
}

func newTestSpooler(t *testing.T, backend driver.Interface) (*Spooler, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Options{Dir: dir, Backend: backend, KeepCompleted: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s, dir
}

func pwgDoc(data string) ipp.Document {
	return ipp.Document{
		Format: "image/pwg-raster",
		Size:   int64(len(data)),
		Data:   strings.NewReader(data),
	}
}

func TestJobLifecycleCompletes(t *testing.T) {
	backend := &recordingBackend{}
	s, dir := newTestSpooler(t, backend)

	id, err := s.Begin(ipp.JobInfo{UserName: "alice", Name: "life", Copies: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddDocument(id, pwgDoc("PWGDATA"), true); err != nil {
		t.Fatal(err)
	}

	waitForState(t, s, id, ipp.JobStateCompleted, 3*time.Second)

	v, ok := s.View(id)
	if !ok || v.User != "alice" || v.Name != "life" {
		t.Fatalf("view = %+v ok=%v", v, ok)
	}
	if backend.count() != 1 {
		t.Fatalf("backend printed %d times", backend.count())
	}
	// The document must be on disk in the spool dir.
	if _, err := os.Stat(filepath.Join(dir, "jobs", "job-000001", "doc-000.bin")); err != nil {
		t.Fatalf("spooled document missing: %v", err)
	}
}

func TestGzipDocumentIsDecompressed(t *testing.T) {
	var zbuf bytes.Buffer
	zw := newGzipWriter(&zbuf)
	zw.Write([]byte("PLAIN-AFTER-GUNZIP"))
	zw.Close()

	backend := &recordingBackend{}
	s, _ := newTestSpooler(t, backend)
	id, _ := s.Begin(ipp.JobInfo{Name: "gz"})
	err := s.AddDocument(id, ipp.Document{
		Format:     "image/pwg-raster",
		Compressed: "gzip",
		Data:       bytes.NewReader(zbuf.Bytes()),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, s, id, ipp.JobStateCompleted, 3*time.Second)
}

func TestCancelPendingJob(t *testing.T) {
	backend := &recordingBackend{}
	s, _ := newTestSpooler(t, backend)

	id, _ := s.Begin(ipp.JobInfo{Name: "cancel-me"})
	if err := s.AddDocument(id, pwgDoc("x"), true); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitForState(t, s, id, ipp.JobStateCanceled, 3*time.Second)
	if backend.count() != 0 {
		t.Fatalf("canceled job reached the backend %d times", backend.count())
	}

	// Canceling a finished job is client-error-not-possible.
	if err := s.Cancel(id); !errors.Is(err, ipp.ErrJobFinished) {
		t.Fatalf("second cancel err = %v, want ErrJobFinished", err)
	}
}

func TestCancelProcessingJob(t *testing.T) {
	backend := &recordingBackend{blockAll: true}
	s, _ := newTestSpooler(t, backend)

	id, _ := s.Begin(ipp.JobInfo{Name: "long-runner"})
	if err := s.AddDocument(id, pwgDoc("big job"), true); err != nil {
		t.Fatal(err)
	}
	// Wait until it is on the backend, then cancel mid-flight.
	waitForState(t, s, id, ipp.JobStateProcessing, 3*time.Second)
	if err := s.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitForState(t, s, id, ipp.JobStateCanceled, 3*time.Second)
}

func TestDriverFailureAborts(t *testing.T) {
	backend := &recordingBackend{failNext: true}
	s, _ := newTestSpooler(t, backend)

	id, _ := s.Begin(ipp.JobInfo{Name: "doomed"})
	_ = s.AddDocument(id, pwgDoc("x"), true)
	waitForState(t, s, id, ipp.JobStateAborted, 3*time.Second)
}

func TestPersistenceRecoveryRequeues(t *testing.T) {
	// Construct the on-disk state a crashed process leaves behind:
	// one pending job with a complete document (crash between queueing
	// and printing) and one held job with no documents (crash while the
	// client was still uploading).
	dir := t.TempDir()
	writeJob := func(id int, meta, docContent string) {
		d := filepath.Join(dir, "jobs", fmt.Sprintf("job-%06d", id))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "meta.json"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
		if docContent != "" {
			if err := os.WriteFile(filepath.Join(d, "doc-000.bin"), []byte(docContent), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeJob(7, `{"id":7,"state":3,"reasons":["none"],"user":"crash","name":"survivor",
		"docs":[{"format":"image/pwg-raster","file":"doc-000.bin","size":9}],
		"created":"2026-01-01T00:00:00Z"}`, "PWGDATA-1")
	writeJob(8, `{"id":8,"state":4,"reasons":["job-incoming"],"user":"crash","name":"empty",
		"docs":[],"created":"2026-01-01T00:00:00Z"}`, "")

	backend2 := &recordingBackend{}
	s2, err := New(Options{Dir: dir, Backend: backend2, KeepCompleted: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s2.Close(c)
	})

	waitForState(t, s2, 7, ipp.JobStateCompleted, 3*time.Second)
	if backend2.count() != 1 {
		t.Fatalf("recovered job printed %d times, want 1", backend2.count())
	}
	// The held job with no documents was aborted, not requeued.
	if v, ok := s2.View(8); !ok || v.State != ipp.JobStateAborted {
		t.Fatalf("held-without-docs job = %+v ok=%v, want aborted", v, ok)
	}
	// The next Begin continues the id sequence.
	id, err := s2.Begin(ipp.JobInfo{Name: "next"})
	if err != nil || id != 9 {
		t.Fatalf("Begin = %d, %v; want 9 (ids continue after recovery)", id, err)
	}
}

func TestSnapshotFiltersAndPrunes(t *testing.T) {
	backend := &recordingBackend{}
	s, _ := newTestSpooler(t, backend) // KeepCompleted=2

	var ids []int32
	for i := 0; i < 4; i++ {
		id, _ := s.Begin(ipp.JobInfo{Name: "p", UserName: "u"})
		_ = s.AddDocument(id, pwgDoc("x"), true)
		ids = append(ids, id)
		waitForState(t, s, id, ipp.JobStateCompleted, 3*time.Second)
	}

	if got := len(s.Snapshot("completed")); got > 2 {
		t.Fatalf("completed history = %d, pruned to 2 expected", got)
	}
	// Oldest jobs are gone entirely.
	for _, id := range ids[:2] {
		if _, ok := s.View(id); ok {
			t.Errorf("job %d should have been pruned", id)
		}
	}
	if _, ok := s.View(ids[3]); !ok {
		t.Error("newest job must survive pruning")
	}
}

func TestUnknownJobErrors(t *testing.T) {
	backend := &recordingBackend{}
	s, _ := newTestSpooler(t, backend)
	if _, err := s.Begin(ipp.JobInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDocument(999, pwgDoc("x"), true); !errors.Is(err, ipp.ErrUnknownJob) {
		t.Fatalf("AddDocument err = %v", err)
	}
	if err := s.Cancel(999); !errors.Is(err, ipp.ErrUnknownJob) {
		t.Fatalf("Cancel err = %v", err)
	}
	if _, ok := s.View(999); ok {
		t.Fatal("View(999) must miss")
	}
}

func waitForState(t *testing.T, s *Spooler, id int32, want int32, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if v, ok := s.View(id); ok && v.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	v, _ := s.View(id)
	t.Fatalf("job %d state = %+v, want %d within %s", id, v, want, within)
}

func TestPauseRejectsNewJobsButLetsQueueFinish(t *testing.T) {
	backend := &recordingBackend{}
	s, _ := newTestSpooler(t, backend)

	// Queue one job first: pausing must not touch it.
	id, err := s.Begin(ipp.JobInfo{UserName: "alice", Name: "before-pause"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddDocument(id, pwgDoc("PWGDATA"), true); err != nil {
		t.Fatal(err)
	}

	s.SetAccepting(false)
	if s.Accepting() {
		t.Fatal("Accepting() = true right after SetAccepting(false)")
	}
	if accepting, _, reasons := s.Status(); accepting || len(reasons) == 0 || reasons[0] != "paused" {
		t.Fatalf("Status while paused = (%v, %v)", accepting, reasons)
	}
	if _, err := s.Begin(ipp.JobInfo{UserName: "bob", Name: "while-paused"}); err != ipp.ErrNotAccepting {
		t.Fatalf("Begin while paused = %v, want ErrNotAccepting", err)
	}

	// The pre-pause job still prints to completion.
	waitForState(t, s, id, ipp.JobStateCompleted, 3*time.Second)
	if backend.count() != 1 {
		t.Fatalf("backend printed %d times, want 1", backend.count())
	}

	s.SetAccepting(true)
	if !s.Accepting() {
		t.Fatal("Accepting() = false right after SetAccepting(true)")
	}
	if accepting, _, reasons := s.Status(); !accepting || len(reasons) != 0 {
		t.Fatalf("Status after resume = (%v, %v)", accepting, reasons)
	}
	id2, err := s.Begin(ipp.JobInfo{UserName: "bob", Name: "after-resume"})
	if err != nil {
		t.Fatalf("Begin after resume: %v", err)
	}
	if err := s.AddDocument(id2, pwgDoc("PWGDATA2"), true); err != nil {
		t.Fatal(err)
	}
	waitForState(t, s, id2, ipp.JobStateCompleted, 3*time.Second)
}
