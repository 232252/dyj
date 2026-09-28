// jobs.go defines the seam between the IPP protocol layer and the
// spooler/driver pipeline. pkg/ipp owns the interface; pkg/spooler
// implements it; cmd/dyj wires them. Neither of the other two imports
// this one's HTTP machinery, so each layer stays testable alone
// (ADR-0002 D4 "无巨石").
package ipp

import (
	"context"
	"errors"
	"io"
	"time"
)

// Job state enums, RFC 8011 §5.3.8 job-state values.
const (
	JobStatePending    int32 = 3
	JobStateHeld       int32 = 4
	JobStateProcessing int32 = 5
	JobStateStopped    int32 = 6
	JobStateCanceled   int32 = 7
	JobStateAborted    int32 = 8
	JobStateCompleted  int32 = 9
)

// JobSink receives documents submitted through IPP and owns their
// lifecycle. All methods must be safe for concurrent use.
type JobSink interface {
	// Begin starts a job in the held state (RFC 8011 §4.2.3): a
	// Create-Job has arrived and documents will follow via AddDocument
	// until last=true. The returned id is final and positive.
	Begin(info JobInfo) (int32, error)

	// AddDocument streams one document into an existing job. When last
	// is true the job becomes printable: the sink queues it for
	// processing and AddDocument returns once the document is durably
	// spooled (the IPP response can then honestly say pending).
	AddDocument(jobID int32, doc Document, last bool) error

	// Cancel aborts a queued or processing job. Canceling a finished
	// job is client-error-not-possible; an unknown id is not-found.
	Cancel(jobID int32) error

	// Snapshot lists jobs: whichJobs is one of "not-completed",
	// "completed" or "all" (RFC 8011 §4.2.6.2 which-jobs), in creation
	// order.
	Snapshot(whichJobs string) []JobView

	// View returns one job by id.
	View(jobID int32) (JobView, bool)

	// Status reports the printer-level dynamic state.
	Status() (accepting bool, queued int32, reasons []string)
}

// JobInfo is the metadata of a new job, extracted from the IPP request.
type JobInfo struct {
	UserName string
	Name     string
	Copies   int
	// Template carries the validated job-template attributes (media,
	// sides, print-color-mode, ...). Keys and values are IPP keywords.
	Template map[string]string
}

// Document is one document of a job.
type Document struct {
	// Format is the document-format (mime type), "application/octet-stream"
	// when the client did not say.
	Format string
	// Compressed is the IPP-level compression the client applied
	// ("gzip" or "" for none). The SINK decompresses (it owns storage);
	// the server only reports what was negotiated.
	Compressed string
	// Data streams the document bytes. It must be fully consumed or
	// closed by the sink.
	Data io.Reader
	// Size is a hint (Content-Length derived); -1 when unknown.
	Size int64
}

// JobView is the spooler's answer for Get-Job-Attributes / Get-Jobs.
type JobView struct {
	ID      int32
	State   int32 // RFC 8011 §5.3.8
	Reasons []string

	Name string
	User string

	// Format of the (single) document, when uniform.
	Format string

	SizeKOctets int32
	Copies      int

	CreatedAt    time.Time
	ProcessingAt time.Time
	CompletedAt  time.Time

	// UpTime is seconds since printer start, used for time-at-*
	// attributes (RFC 8011 §5.3.7: when printer-current-time is
	// supported, time-at-* are dateTime; the server converts).
	CreatedUp    int32
	ProcessingUp int32
	CompletedUp  int32
}

// Errors a JobSink may return; the server maps them to IPP status codes.
var (
	// ErrUnknownJob → client-error-not-found (0x0406).
	ErrUnknownJob = errors.New("ipp: unknown job id")
	// ErrJobFinished → client-error-not-possible (0x0503).
	ErrJobFinished = errors.New("ipp: job already finished")
	// ErrNotAccepting → server-error-not-accepting-jobs (0x0506).
	ErrNotAccepting = errors.New("ipp: printer is not accepting jobs")
)

// JobContext returns the context whose cancellation a driver must honor
// for a processing job. Sinks that support cancelation expose it through
// this interface; the server uses it for nothing today but tests do.
type JobContext interface {
	JobCtx(jobID int32) (context.Context, bool)
}
