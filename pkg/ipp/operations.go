package ipp

import (
	"compress/gzip"
	"errors"
	"fmt"
	"strconv"

	"github.com/232252/dyj/pkg/attributes"
)

// Operation ids, RFC 8011 §4 / IANA registry.
const (
	OpPrintJob             uint16 = 0x0002
	OpValidateJob          uint16 = 0x0004
	OpCreateJob            uint16 = 0x0005
	OpSendDocument         uint16 = 0x0006
	OpCancelJob            uint16 = 0x0008
	OpGetJobAttributes     uint16 = 0x0009
	OpGetJobs              uint16 = 0x000A
	OpGetPrinterAttributes uint16 = 0x000B
	OpIdentifyPrinter      uint16 = 0x003C
)

// dispatch routes a decoded request to its operation handler.
func (s *Server) dispatch(msg *attributes.Message, r *requestContext) *Response {
	switch msg.Code {
	case OpGetPrinterAttributes:
		return s.getPrinterAttributes(msg)
	case OpValidateJob:
		return s.validateJob(msg)
	case OpPrintJob:
		return s.printJob(msg, r, true)
	case OpCreateJob:
		return s.createJob(msg)
	case OpSendDocument:
		return s.sendDocument(msg, r)
	case OpGetJobs:
		return s.getJobs(msg)
	case OpGetJobAttributes:
		return s.getJobAttributes(msg)
	case OpCancelJob:
		return s.cancelJob(msg)
	case OpIdentifyPrinter:
		return s.identifyPrinter(msg)
	default:
		return nil
	}
}

// getPrinterAttributes (0x000B) — the operation Windows, CUPS, iOS and
// macOS all issue before they will add a printer.
func (s *Server) getPrinterAttributes(msg *attributes.Message) *Response {
	want := ExpandRequested(RequestedAttributes(msg))
	attrs := s.BuildPrinterAttrs(s.printer, want)

	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{
		{Tag: GroupOperation, Attrs: BuildOperationAttrs()},
		{Tag: GroupPrinter, Attrs: attrs},
	}
	return resp
}

// validateJob (0x0004) checks a would-be job without creating one.
// RFC 8011 §4.2.2: same validation as Print-Job, minus the spooling.
func (s *Server) validateJob(msg *attributes.Message) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	if r := s.rejectTemplate(msg); r != nil {
		return r
	}
	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{{Tag: GroupOperation, Attrs: BuildOperationAttrs()}}
	return resp
}

// printJob (0x0002): Begin + AddDocument(last=true) in one shot.
func (s *Server) printJob(msg *attributes.Message, r *requestContext, last bool) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	if r := s.rejectTemplate(msg); r != nil {
		return r
	}

	op := msg.Operation()
	compression := attributes.GroupFirstString(op, "compression")

	doc, err := s.openDocument(r, compression)
	if err != nil {
		return s.errorResponse(msg.RequestID, StatusErrorBadRequest, err.Error())
	}
	doc.Format = documentFormat(msg)

	id, err := s.sink.Begin(s.jobInfo(msg))
	if err == nil {
		err = s.sink.AddDocument(id, doc, last)
		if err != nil {
			_ = s.sink.Cancel(id)
		}
	}
	if err != nil {
		return s.sinkError(msg.RequestID, err)
	}

	return s.jobResponse(msg.RequestID, id, JobStatePending)
}

// createJob (0x0005): begin a multi-document job in the held state.
func (s *Server) createJob(msg *attributes.Message) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	if r := s.rejectTemplate(msg); r != nil {
		return r
	}
	id, err := s.sink.Begin(s.jobInfo(msg))
	if err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	return s.jobResponse(msg.RequestID, id, JobStateHeld)
}

// sendDocument (0x0006): add one document to a held job.
func (s *Server) sendDocument(msg *attributes.Message, r *requestContext) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	op := msg.Operation()
	jobID, ok := attributes.GroupInt(op, "job-id")
	if !ok || jobID <= 0 {
		return s.errorResponse(msg.RequestID, StatusErrorBadRequest, "dyj: Send-Document requires job-id")
	}
	last, _ := attributes.GroupBool(op, "last-document")

	if fatal, _ := s.validateTemplate(msg); len(fatal) > 0 {
		return s.unsupportedResponse(msg.RequestID, fatal)
	}
	compression := attributes.GroupFirstString(op, "compression")
	doc, err := s.openDocument(r, compression)
	if err != nil {
		return s.errorResponse(msg.RequestID, StatusErrorBadRequest, err.Error())
	}
	doc.Format = documentFormat(msg)

	if _, ok := s.sink.View(int32(jobID)); !ok {
		return s.sinkError(msg.RequestID, ErrUnknownJob)
	}
	if err := s.sink.AddDocument(int32(jobID), doc, last); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	return s.jobResponse(msg.RequestID, int32(jobID), s.stateAfterSend(jobID, last))
}

// stateAfterSend reports the job state the response should carry after a
// Send-Document. RFC 8011: held until last-document, then pending.
func (s *Server) stateAfterSend(jobID int32, last bool) int32 {
	if !last {
		return JobStateHeld
	}
	if v, ok := s.sink.View(int32(jobID)); ok {
		return v.State
	}
	return JobStatePending
}

// getJobs (0x000A).
func (s *Server) getJobs(msg *attributes.Message) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	op := msg.Operation()
	which := attributes.GroupFirstString(op, "which-jobs")
	switch which {
	case "", "not-completed", "completed", "all":
	default:
		return s.errorResponse(msg.RequestID, StatusErrorClientUnsupported, "dyj: unknown which-jobs value "+which)
	}
	want := RequestedAttributes(msg)

	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{{Tag: GroupOperation, Attrs: BuildOperationAttrs()}}
	for _, v := range s.sink.Snapshot(which) {
		resp.Groups = append(resp.Groups, Group{Tag: GroupJob, Attrs: s.jobAttrs(v, want)})
	}
	return resp
}

// getJobAttributes (0x0009).
func (s *Server) getJobAttributes(msg *attributes.Message) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	jobID, ok := attributes.GroupInt(msg.Operation(), "job-id")
	if !ok || jobID <= 0 {
		return s.errorResponse(msg.RequestID, StatusErrorBadRequest, "dyj: Get-Job-Attributes requires job-id")
	}
	v, ok := s.sink.View(int32(jobID))
	if !ok {
		return s.sinkError(msg.RequestID, ErrUnknownJob)
	}
	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{
		{Tag: GroupOperation, Attrs: BuildOperationAttrs()},
		{Tag: GroupJob, Attrs: s.jobAttrs(v, RequestedAttributes(msg))},
	}
	return resp
}

// cancelJob (0x0008).
func (s *Server) cancelJob(msg *attributes.Message) *Response {
	if err := s.checkSink(); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	jobID, ok := attributes.GroupInt(msg.Operation(), "job-id")
	if !ok || jobID <= 0 {
		return s.errorResponse(msg.RequestID, StatusErrorBadRequest, "dyj: Cancel-Job requires job-id")
	}
	if err := s.sink.Cancel(int32(jobID)); err != nil {
		return s.sinkError(msg.RequestID, err)
	}
	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{{Tag: GroupOperation, Attrs: BuildOperationAttrs()}}
	return resp
}

// identifyPrinter (0x003C): REQUIRED by IPP Everywhere PWG 5100.14. A
// physical printer flashes or beeps; a shared endpoint logs instead, so
// operators can correlate the request. Success keeps the add-printer
// flow unblocked on clients that probe this.
func (s *Server) identifyPrinter(msg *attributes.Message) *Response {
	s.log.Printf("ipp: Identify-Printer requested (actions=%v); no physical device, no-op",
		attributes.GroupStrings(msg.Operation(), "identify-actions"))
	resp := NewResponse(StatusOK, msg.RequestID)
	resp.Groups = []Group{{Tag: GroupOperation, Attrs: BuildOperationAttrs()}}
	return resp
}

// ---------------------------------------------------------------------------
// helpers shared by the operation handlers
// ---------------------------------------------------------------------------

// checkSink reports the no-backend case with the honest 0x0506 instead of
// accepting a document it cannot print.
func (s *Server) checkSink() error {
	if s.sink == nil {
		return errNoSink
	}
	return nil
}

var errNoSink = errors.New("dyj: no print backend wired up yet")

// openDocument wraps the request's document stream, decompressing when
// the client declared IPP-level compression (RFC 8011 §4.2.1).
func (s *Server) openDocument(r *requestContext, compression string) (Document, error) {
	doc := Document{Data: r.body, Size: r.contentLength}
	switch compression {
	case "", "none":
		return doc, nil
	case "gzip":
		zr, err := gzip.NewReader(r.body)
		if err != nil {
			return Document{}, fmt.Errorf("dyj: bad gzip document stream: %w", err)
		}
		doc.Compressed = compression
		doc.Data = zr
		doc.Size = -1
		return doc, nil
	default:
		return Document{}, fmt.Errorf("dyj: unsupported compression %q (supported: none, gzip)", compression)
	}
}

// documentFormat returns the declared document-format, with the RFC
// 8011 §4.2.1.4 default.
func documentFormat(msg *attributes.Message) string {
	if f := attributes.GroupFirstString(msg.Operation(), "document-format"); f != "" {
		return f
	}
	return "application/octet-stream"
}

// jobInfo extracts job metadata from the request groups.
func (s *Server) jobInfo(msg *attributes.Message) JobInfo {
	op := msg.Operation()
	info := JobInfo{
		UserName: attributes.GroupFirstString(op, "requesting-user-name"),
		Name:     attributes.GroupFirstString(op, "job-name"),
		Template: map[string]string{},
	}
	if info.Name == "" {
		info.Name = "dyj job"
	}
	for _, name := range templateAttributeNames {
		if v := attributes.GroupFirstString(msg.Job(), name); v != "" {
			info.Template[name] = v
		}
	}
	// RFC 8011 default is 1. Leaving this at 0 made the recent-prints
	// column show "0" for every job that omitted the attribute
	// (research/12 §3).
	info.Copies = 1
	if copies, ok := attributes.GroupInt(msg.Job(), "copies"); ok && copies > 0 {
		info.Copies = int(copies)
	}
	return info
}

// jobResponse builds the standard job-attributes group of a job-creation
// response (RFC 8011 §4.2.1: job-id, job-uri, job-state,
// job-state-reasons are REQUIRED).
func (s *Server) jobResponse(requestID int32, id int32, state int32) *Response {
	resp := NewResponse(StatusOK, requestID)
	resp.Groups = []Group{
		{Tag: GroupOperation, Attrs: BuildOperationAttrs()},
		{Tag: GroupJob, Attrs: []Attr{
			{Tag: TagInteger, Name: "job-id", Values: [][]byte{Int(id)}},
			{Tag: TagURI, Name: "job-uri", Values: [][]byte{Text(s.jobURI(id))}},
			{Tag: TagEnum, Name: "job-state", Values: [][]byte{Enum(state)}},
			{Tag: TagKeyword, Name: "job-state-reasons", Values: [][]byte{Text(jobReasonFor(state))}},
		}},
	}
	return resp
}

func (s *Server) jobURI(id int32) string {
	base := s.printer.primaryURI()
	return base + "/" + strconv.Itoa(int(id))
}

func jobReasonFor(state int32) string {
	switch state {
	case JobStateHeld:
		return "job-data-insufficient"
	case JobStatePending:
		return "none"
	case JobStateProcessing:
		return "job-printing"
	default:
		return "none"
	}
}

// jobAttrs renders a JobView as one job-attributes group, filtered by
// requested-attributes.
func (s *Server) jobAttrs(v JobView, want []string) []Attr {
	up := int32(uptime())
	attrs := []Attr{
		{Tag: TagInteger, Name: "job-id", Values: [][]byte{Int(v.ID)}},
		{Tag: TagURI, Name: "job-uri", Values: [][]byte{Text(s.jobURI(v.ID))}},
		{Tag: TagEnum, Name: "job-state", Values: [][]byte{Enum(v.State)}},
		{Tag: TagKeyword, Name: "job-state-reasons", Values: keywordsOrNone(v.Reasons)},
		{Tag: TagName, Name: "job-name", Values: [][]byte{Text(v.Name)}},
		{Tag: TagName, Name: "job-originating-user-name", Values: [][]byte{Text(v.User)}},
		{Tag: TagMimeType, Name: "document-format", Values: [][]byte{Text(v.Format)}},
		{Tag: TagInteger, Name: "job-k-octets", Values: [][]byte{Int(v.SizeKOctets)}},
		{Tag: TagInteger, Name: "time-at-creation", Values: [][]byte{Int(v.CreatedUp)}},
		{Tag: TagInteger, Name: "job-printer-up-time", Values: [][]byte{Int(up)}},
	}
	if !v.ProcessingAt.IsZero() {
		attrs = append(attrs, Attr{Tag: TagInteger, Name: "time-at-processing", Values: [][]byte{Int(v.ProcessingUp)}})
	}
	if !v.CompletedAt.IsZero() {
		attrs = append(attrs, Attr{Tag: TagInteger, Name: "time-at-completed", Values: [][]byte{Int(v.CompletedUp)}})
	}
	return FilterAttrs(attrs, want)
}

func keywordsOrNone(v []string) [][]byte {
	if len(v) == 0 {
		return [][]byte{Text("none")}
	}
	out := make([][]byte, len(v))
	for i, s := range v {
		out[i] = Text(s)
	}
	return out
}

// errorResponse builds a status-only IPP response with a status-message.
func (s *Server) errorResponse(requestID int32, status int16, msg string) *Response {
	resp := NewResponse(status, requestID)
	resp.Groups = []Group{{Tag: GroupOperation, Attrs: append(BuildOperationAttrs(),
		Attr{Tag: TagText, Name: "status-message", Values: [][]byte{Text(msg)}})}}
	return resp
}

// sinkError maps JobSink errors to IPP status codes.
func (s *Server) sinkError(requestID int32, err error) *Response {
	switch {
	case errors.Is(err, errNoSink):
		return s.errorResponse(requestID, 0x0506, err.Error()) // printer-not-accepting-jobs
	case errors.Is(err, ErrUnknownJob):
		return s.errorResponse(requestID, StatusErrorNotFound, err.Error())
	case errors.Is(err, ErrJobFinished):
		return s.errorResponse(requestID, StatusErrorNotPossible, err.Error())
	case errors.Is(err, ErrNotAccepting):
		return s.errorResponse(requestID, 0x0506, err.Error())
	default:
		return s.errorResponse(requestID, StatusErrorDocumentAccess, err.Error())
	}
}

// uptime is a package hook so tests can freeze time-at-creation values.
var uptime = func() int64 { return 0 }
