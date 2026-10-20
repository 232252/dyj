// Package spooler owns the job lifecycle: accept documents, spool them
// to disk as they stream in, hand jobs to the driver backend one at a
// time, persist state so a crash never loses a queued job, and keep the
// history bounded.
//
// State machine (states are RFC 8011 §5.3.8 job-state values):
//
//	Begin ──► held(4) ──docs──► pending(3) ──worker──► processing(5) ─┬─► completed(9)
//	                     ▲                        │                   ├─► canceled(7)
//	                     └────────────────────────┘                   └─► aborted(8)
//	                                                     (driver error)
//
// Persistence is one meta.json per job directory, rewritten atomically
// (write temp + rename) at every transition, with document entries
// appended only AFTER the document file is complete — a crash mid-write
// can therefore lose at most the document currently being received,
// never a half-logged one.
package spooler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/232252/dyj/pkg/driver"
	"github.com/232252/dyj/pkg/ipp"
)

// Options configures a Spooler.
type Options struct {
	// Dir is the spool root (created on demand).
	Dir string
	// Backend prints spooled documents.
	Backend driver.Interface
	// KeepCompleted bounds the finished-job history (default 20).
	KeepCompleted int
	// IDBase lifts the job-id counter so several per-printer spoolers can
	// share one admin job list without colliding (printer i gets
	// i*1_000_000). 0 keeps the classic 1..N sequence.
	IDBase int32
	// Logger, optional.
	Logger *log.Logger
}

// document is one spooled document record.
type document struct {
	Format string `json:"format"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
}

// job is the persisted state of one job.
type job struct {
	ID       int32      `json:"id"`
	State    int32      `json:"state"`
	Reasons  []string   `json:"reasons"`
	User     string     `json:"user"`
	Name     string     `json:"name"`
	Media    string     `json:"media,omitempty"`
	Copies   int        `json:"copies,omitempty"`
	Color    bool       `json:"color,omitempty"`
	Sides    string     `json:"sides,omitempty"`
	Docs     []document `json:"docs"`
	Created  time.Time  `json:"created"`
	Proc     time.Time  `json:"processing,omitempty"`
	Done     time.Time  `json:"completed,omitempty"`
	Accepted bool       `json:"accepted"` // has at least one full document
}

// Spooler implements ipp.JobSink.
type Spooler struct {
	opts Options
	log  *log.Logger

	mu     sync.Mutex
	jobs   map[int32]*job
	order  []int32
	nextID int32

	// accepting is the operator pause switch (admin API "暂停共享"):
	// false rejects new jobs at Begin and reports printer-state-reason
	// "paused"; jobs already queued keep printing.
	accepting bool

	queue   chan int32
	wake    chan struct{}
	cancels map[int32]context.CancelFunc
	start   time.Time
	closed  bool
}

// New creates the spooler, recovers any jobs from a previous run and
// starts the single print worker. Recovery policy: a held job with
// complete documents and a processing job (crashed mid-print) both
// return to pending and print again — documents in meta.json are always
// complete files, so replaying is safe.
func New(opts Options) (*Spooler, error) {
	if opts.Backend == nil {
		return nil, errors.New("spooler: no backend configured")
	}
	if opts.Dir == "" {
		return nil, errors.New("spooler: empty spool directory")
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	s := &Spooler{
		log:       logger,
		jobs:      map[int32]*job{},
		accepting: true,
		queue:     make(chan int32, 1024),
		wake:      make(chan struct{}, 1),
		cancels:   map[int32]context.CancelFunc{},
		start:     time.Now(),
	}
	// Normalise BEFORE copying into s.opts: s.opts is a value copy, so
	// fixing the local variable afterwards would leave KeepCompleted at
	// zero and prune() would erase the entire finished-job history.
	if opts.KeepCompleted <= 0 {
		opts.KeepCompleted = 20
	}
	s.opts = opts
	if err := os.MkdirAll(s.jobsDir(), 0o755); err != nil {
		return nil, fmt.Errorf("spooler: creating %s: %w", s.jobsDir(), err)
	}
	if err := s.recover(); err != nil {
		return nil, err
	}
	if s.nextID < opts.IDBase {
		s.nextID = opts.IDBase
	}
	go s.worker()
	return s, nil
}

func (s *Spooler) jobsDir() string { return filepath.Join(s.opts.Dir, "jobs") }
func (s *Spooler) jobDir(id int32) string {
	return filepath.Join(s.jobsDir(), fmt.Sprintf("job-%06d", id))
}

// recover reloads persisted jobs and requeues printable ones.
func (s *Spooler) recover() error {
	entries, err := os.ReadDir(s.jobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "job-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.jobsDir(), e.Name(), "meta.json"))
		if err != nil {
			s.log.Printf("spooler: skipping unreadable job dir %s: %v", e.Name(), err)
			continue
		}
		var j job
		if err := json.Unmarshal(data, &j); err != nil {
			s.log.Printf("spooler: skipping corrupt meta in %s: %v", e.Name(), err)
			continue
		}
		switch j.State {
		case ipp.JobStateHeld:
			// Held with zero documents = the client vanished mid-job.
			if len(j.Docs) == 0 {
				j.State = ipp.JobStateAborted
				j.Reasons = []string{"job-canceled-by-system"}
				j.Done = time.Now()
			} else {
				j.State = ipp.JobStatePending
			}
		case ipp.JobStatePending, ipp.JobStateProcessing:
			j.State = ipp.JobStatePending
		default:
		}
		s.jobs[j.ID] = &j
		s.order = append(s.order, j.ID)
		if j.ID > s.nextID {
			s.nextID = j.ID
		}
		if j.State == ipp.JobStatePending {
			select {
			case s.queue <- j.ID:
			default:
				s.log.Printf("spooler: job %d recovered but queue is full", j.ID)
			}
		}
	}
	slices.Sort(s.order)
	return nil
}

// Begin implements ipp.JobSink.
func (s *Spooler) Begin(info ipp.JobInfo) (int32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ipp.ErrNotAccepting
	}
	if !s.accepting {
		return 0, ipp.ErrNotAccepting
	}
	s.nextID++
	id := s.nextID
	j := &job{
		ID:      id,
		State:   ipp.JobStateHeld,
		Reasons: []string{"job-incoming"},
		User:    info.UserName,
		Name:    info.Name,
		Media:   info.Template["media"],
		Copies:  info.Copies,
		Color:   info.Template["print-color-mode"] == "color",
		Sides:   info.Template["sides"],
		Created: time.Now(),
	}
	if err := os.MkdirAll(s.jobDir(id), 0o755); err != nil {
		return 0, fmt.Errorf("spooler: creating job dir: %w", err)
	}
	if err := s.persistLocked(j); err != nil {
		return 0, err
	}
	s.jobs[id] = j
	s.order = append(s.order, id)
	return id, nil
}

// AddDocument implements ipp.JobSink: streams the document to disk and,
// when last=true, queues the job for printing.
func (s *Spooler) AddDocument(id int32, doc ipp.Document, last bool) error {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ipp.ErrUnknownJob
	}
	switch j.State {
	case ipp.JobStateHeld, ipp.JobStatePending:
	default:
		s.mu.Unlock()
		return ipp.ErrJobFinished
	}
	// Document bytes arrive ALREADY DECOMPRESSED: the IPP server owns
	// the raw wire stream and unwraps compression exactly once
	// (ipp.openDocument). doc.Compressed is metadata for the record,
	// not an instruction — decompressing here again would consume the
	// already-plain pixels as a gzip header (caught by the real CUPS
	// client: gzip-of-plain fails with "invalid header" and the job
	// stuck in held).
	rdr := doc.Data
	index := len(j.Docs)
	format := doc.Format
	if format == "" || format == "application/octet-stream" {
		format = "application/octet-stream"
	}
	s.mu.Unlock()

	if err := os.MkdirAll(s.jobDir(id), 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("doc-%03d.bin", index)
	path := filepath.Join(s.jobDir(id), name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("spooler: creating document file: %w", err)
	}
	size, err := io.Copy(f, rdr)
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("spooler: spooling document: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok = s.jobs[id]
	if !ok {
		return ipp.ErrUnknownJob
	}
	j.Docs = append(j.Docs, document{Format: format, File: name, Size: size})
	j.Accepted = true
	j.Reasons = []string{"none"}
	if last {
		j.State = ipp.JobStatePending
	}
	if err := s.persistLocked(j); err != nil {
		return err
	}
	if last {
		select {
		case s.queue <- id:
		default:
			s.log.Printf("spooler: queue full, job %d waits", id)
		}
	}
	return nil
}

// Cancel implements ipp.JobSink.
func (s *Spooler) Cancel(id int32) error {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ipp.ErrUnknownJob
	}
	switch j.State {
	case ipp.JobStateHeld, ipp.JobStatePending:
		j.State = ipp.JobStateCanceled
		j.Reasons = []string{"job-canceled-by-user"}
		j.Done = time.Now()
		err := s.persistLocked(j)
		s.mu.Unlock()
		s.prune()
		return err
	case ipp.JobStateProcessing:
		if cancel, ok := s.cancels[id]; ok {
			cancel()
		}
		s.mu.Unlock()
		// The worker observes the cancel and finalizes the state.
		return nil
	default:
		s.mu.Unlock()
		return ipp.ErrJobFinished
	}
}

// Snapshot implements ipp.JobSink.
func (s *Spooler) Snapshot(whichJobs string) []ipp.JobView {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ipp.JobView, 0, len(s.order))
	for _, id := range s.order {
		j := s.jobs[id]
		if j == nil {
			continue
		}
		if !stateMatches(j.State, whichJobs) {
			continue
		}
		out = append(out, s.viewLocked(j))
	}
	return out
}

// View implements ipp.JobSink.
func (s *Spooler) View(id int32) (ipp.JobView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	if j == nil {
		return ipp.JobView{}, false
	}
	return s.viewLocked(j), true
}

// Status implements ipp.JobSink (via ipp.StatusProvider).
func (s *Spooler) Status() (bool, int32, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, 0, []string{"printer-stopped"}
	}
	queued := 0
	for _, id := range s.order {
		switch s.jobs[id].State {
		case ipp.JobStatePending, ipp.JobStateProcessing, ipp.JobStateHeld:
			queued++
		}
	}
	if !s.accepting {
		return false, int32(queued), []string{"paused"}
	}
	return true, int32(queued), nil
}

// SetAccepting is the operator pause switch. Pausing rejects new jobs
// (server-error-not-accepting-jobs) and advertises the "paused"
// printer-state-reason; queued jobs finish printing normally.
func (s *Spooler) SetAccepting(accepting bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accepting = accepting
}

// Accepting reports the current pause switch state.
func (s *Spooler) Accepting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepting && !s.closed
}

func stateMatches(state int32, which string) bool {
	finished := state == ipp.JobStateCompleted || state == ipp.JobStateCanceled || state == ipp.JobStateAborted
	switch which {
	case "completed":
		return finished
	case "all":
		return true
	default: // "not-completed" and ""
		return !finished
	}
}

func (s *Spooler) viewLocked(j *job) ipp.JobView {
	v := ipp.JobView{
		ID:      j.ID,
		State:   j.State,
		Reasons: j.Reasons,
		Name:    j.Name,
		User:    j.User,
		Copies:  j.Copies,
	}
	if len(j.Docs) > 0 {
		v.Format = j.Docs[0].Format
		var total int64
		for _, d := range j.Docs {
			total += d.Size
		}
		v.SizeKOctets = int32(total / 1024)
	}
	v.CreatedAt, v.ProcessingAt, v.CompletedAt = j.Created, j.Proc, j.Done
	v.CreatedUp = int32(j.Created.Sub(s.start) / time.Second)
	if v.CreatedUp < 0 {
		v.CreatedUp = 0
	}
	if !j.Proc.IsZero() {
		v.ProcessingUp = int32(j.Proc.Sub(s.start) / time.Second)
	}
	if !j.Done.IsZero() {
		v.CompletedUp = int32(j.Done.Sub(s.start) / time.Second)
	}
	return v
}

// worker prints queued jobs one at a time. A physical printer is a
// serial resource; parallelism would only reorder pages.
func (s *Spooler) worker() {
	for id := range s.queue {
		if s.stopIfClosed(id) {
			return
		}
		s.printJob(id)
	}
}

func (s *Spooler) stopIfClosed(id int32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		// Put it back? No: Close() requeues unfinished jobs itself.
		return true
	}
	j := s.jobs[id]
	return j == nil || j.State != ipp.JobStatePending
}

func (s *Spooler) printJob(id int32) {
	s.mu.Lock()
	j := s.jobs[id]
	if j == nil || j.State != ipp.JobStatePending {
		s.mu.Unlock()
		return
	}
	j.State = ipp.JobStateProcessing
	j.Reasons = []string{"job-printing"}
	j.Proc = time.Now()
	_ = s.persistLocked(j)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[id] = cancel
	docs := append([]document(nil), j.Docs...)
	pj := driver.PrintJob{
		ID: j.ID, User: j.User, Name: j.Name,
		Media: j.Media, Copies: j.Copies, Color: j.Color, Sides: j.Sides,
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, id)
		s.mu.Unlock()
		cancel()
	}()

	var printErr error
	for _, d := range docs {
		doc := driver.Document{
			Format: d.Format,
			Size:   d.Size,
			Open: func() (io.ReadCloser, error) {
				return os.Open(filepath.Join(s.jobDir(id), d.File))
			},
		}
		if err := s.opts.Backend.Print(ctx, pj, doc); err != nil {
			printErr = err
			break
		}
		if ctx.Err() != nil {
			printErr = ctx.Err()
			break
		}
	}

	s.mu.Lock()
	j = s.jobs[id]
	if j == nil {
		s.mu.Unlock()
		return
	}
	j.Done = time.Now()
	switch {
	case ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled):
		j.State = ipp.JobStateCanceled
		j.Reasons = []string{"job-canceled-by-user"}
	case printErr != nil:
		j.State = ipp.JobStateAborted
		j.Reasons = []string{"aborted-by-system"}
		s.log.Printf("spooler: job %d aborted: %v", id, printErr)
	default:
		j.State = ipp.JobStateCompleted
		j.Reasons = []string{"job-completed-successfully"}
	}
	_ = s.persistLocked(j)
	s.mu.Unlock()
	s.prune()
}

// persistLocked atomically rewrites meta.json.
func (s *Spooler) persistLocked(j *job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.jobDir(j.ID), "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("spooler: persisting meta: %w", err)
	}
	return os.Rename(tmp, filepath.Join(s.jobDir(j.ID), "meta.json"))
}

// prune drops the oldest finished jobs beyond KeepCompleted.
func (s *Spooler) prune() {
	s.mu.Lock()
	var finished []int32
	for _, id := range s.order {
		switch s.jobs[id].State {
		case ipp.JobStateCompleted, ipp.JobStateCanceled, ipp.JobStateAborted:
			finished = append(finished, id)
		}
	}
	var remove []int32
	for len(finished) > s.opts.KeepCompleted {
		remove = append(remove, finished[0])
		finished = finished[1:]
	}
	for _, id := range remove {
		delete(s.jobs, id)
		for i, oid := range s.order {
			if oid == id {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		if err := os.RemoveAll(s.jobDir(id)); err != nil {
			s.log.Printf("spooler: pruning job %d: %v", id, err)
		}
		s.mu.Lock()
	}
	s.mu.Unlock()
}

// Close stops accepting jobs and waits for the current job (bounded by
// the driver), then marks every unfinished job aborted.
func (s *Spooler) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	pending := append([]int32(nil), s.order...)
	s.mu.Unlock()

	for _, id := range pending {
		s.mu.Lock()
		j := s.jobs[id]
		if j == nil {
			s.mu.Unlock()
			continue
		}
		finishing := j.State == ipp.JobStateProcessing
		if j.State == ipp.JobStateHeld || j.State == ipp.JobStatePending {
			j.State = ipp.JobStateAborted
			j.Reasons = []string{"shutdown"}
			j.Done = time.Now()
			_ = s.persistLocked(j)
		}
		s.mu.Unlock()
		if finishing {
			s.Cancel(id)
		}
	}

	// Wait for the worker to drain, bounded by ctx.
	done := make(chan struct{})
	go func() {
		// The worker only picks Pending jobs; once none are pending it
		// blocks on the queue. Poll for the processing job to finish.
		for {
			s.mu.Lock()
			active := false
			for _, j := range s.jobs {
				if j.State == ipp.JobStateProcessing {
					active = true
				}
			}
			s.mu.Unlock()
			if !active {
				close(done)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
