// Package client speaks the client side of IPP against a printer URI.
//
// It shares pkg/attributes with the server — the same encoder builds
// requests and the same decoder parses responses, so a protocol bug on
// one side cannot hide from the other (both directions are exercised by
// the E2E test).
package client

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sq199/dyj/pkg/attributes"
)

// StatusOK is the successful-ok status code (RFC 8011 appendix B).
const StatusOK uint16 = 0x0000

// Client is one IPP printer endpoint.
type Client struct {
	// URI is the printer URI, ipp://host:port/path or ipps://....
	URI string
	// User is the requesting-user-name sent with every operation.
	User string
	// HTTP is the transport (a custom client may skip TLS
	// verification for self-signed ipps printers).
	HTTP *http.Client
}

// Parse builds a Client for a printer URI.
func Parse(uri, user string) (*Client, error) {
	c, err := parse(uri, user, false)
	return c, err
}

// ParseInsecure builds a Client that accepts self-signed ipps://
// certificates (the AirPrint-class deployment: printers generate their
// own cert at first boot).
func ParseInsecure(uri, user string) (*Client, error) {
	return parse(uri, user, true)
}

func parse(uri, user string, insecure bool) (*Client, error) {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok || (scheme != "ipp" && scheme != "ipps") {
		return nil, fmt.Errorf("client: URI %q must be ipp:// or ipps://", uri)
	}
	hostport, path, found := strings.Cut(rest, "/")
	if hostport == "" {
		return nil, fmt.Errorf("client: URI %q has no host", uri)
	}
	if !found || path == "" {
		return nil, fmt.Errorf("client: URI %q has no resource path (want e.g. ipp://host:631/ipp/print)", uri)
	}
	if _, _, err := splitHostPort(hostport); err != nil {
		return nil, fmt.Errorf("client: URI %q has a malformed host:port", uri)
	}
	transport := &http.Transport{}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // self-signed printer cert
	}
	return &Client{URI: uri, User: user, HTTP: &http.Client{Timeout: 10 * time.Minute, Transport: transport}}, nil
}

func splitHostPort(hostport string) (string, string, error) {
	i := strings.LastIndex(hostport, ":")
	if i < 0 {
		return hostport, "", fmt.Errorf("no port")
	}
	return hostport[:i], hostport[i+1:], nil
}

// httpURL maps the ipp(s) scheme onto http(s) for the transport.
func (c *Client) httpURL() string {
	scheme := "http"
	if strings.HasPrefix(c.URI, "ipps://") {
		scheme = "https"
	}
	return scheme + "://" + strings.TrimPrefix(strings.TrimPrefix(c.URI, "ipp://"), "ipps://")
}

// PrintOptions are the job-template attributes `dyj print` can set.
type PrintOptions struct {
	JobName string
	Copies  int
	Media   string // PWG media name
	Color   bool
	Fidelity bool
	// Gzip compresses the document stream and declares
	// compression=gzip (RFC 8011 §4.2.1). Worth it for large rasters.
	Gzip bool
}

// GetPrinterAttributes queries the printer. The full response message
// is returned; use attributes helpers to pull fields out.
func (c *Client) GetPrinterAttributes(want ...string) (*attributes.Message, error) {
	opAttrs := []attributes.Attribute{
		attributes.URI("printer-uri", c.URI),
	}
	if len(want) > 0 {
		opAttrs = append(opAttrs, attributes.Keywords("requested-attributes", want...))
	}
	msg, err := c.roundTrip(0x000B, opAttrs, nil, nil, false)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// PrintJob submits one document and returns the new job id.
func (c *Client) PrintJob(doc io.Reader, size int64, format string, opt PrintOptions) (int32, error) {
	if opt.JobName == "" {
		opt.JobName = "dyj job"
	}
	opAttrs := []attributes.Attribute{
		attributes.URI("printer-uri", c.URI),
		attributes.Name("job-name", opt.JobName),
		attributes.MimeType("document-format", format),
	}
	if opt.Gzip {
		opAttrs = append(opAttrs, attributes.Keyword("compression", "gzip"))
	}
	if c.User != "" {
		opAttrs = append(opAttrs, attributes.Name("requesting-user-name", c.User))
	}
	if opt.Fidelity {
		opAttrs = append(opAttrs, attributes.Bool("ipp-attribute-fidelity", true))
	}
	var jobAttrs []attributes.Attribute
	if opt.Copies > 1 {
		jobAttrs = append(jobAttrs, attributes.Integer("copies", int32(opt.Copies)))
	}
	if opt.Media != "" {
		jobAttrs = append(jobAttrs, attributes.Keyword("media", opt.Media))
	}
	if opt.Color {
		jobAttrs = append(jobAttrs, attributes.Keyword("print-color-mode", "color"))
	}

	msg, err := c.roundTrip(0x0002, opAttrs, jobAttrs, doc, opt.Gzip)
	if err != nil {
		return 0, err
	}
	if msg.Code != StatusOK {
		return 0, fmt.Errorf("client: Print-Job failed: %s (0x%04X)", statusMessage(msg), msg.Code)
	}
	id, ok := attributes.GroupInt(msg.Job(), "job-id")
	if !ok {
		return 0, fmt.Errorf("client: Print-Job response carries no job-id")
	}
	return id, nil
}

// JobRow is one job from Get-Jobs.
type JobRow struct {
	ID     int32
	State  int32
	Name   string
	User   string
	Format string
}

// Jobs lists jobs: which is "not-completed", "completed" or "all".
func (c *Client) Jobs(which string) ([]JobRow, error) {
	opAttrs := []attributes.Attribute{attributes.URI("printer-uri", c.URI)}
	if which != "" {
		opAttrs = append(opAttrs, attributes.Keyword("which-jobs", which))
	}
	msg, err := c.roundTrip(0x000A, opAttrs, nil, nil, false)
	if err != nil {
		return nil, err
	}
	if msg.Code != StatusOK {
		return nil, fmt.Errorf("client: Get-Jobs failed: %s (0x%04X)", statusMessage(msg), msg.Code)
	}
	var out []JobRow
	for i := range msg.Groups {
		g := &msg.Groups[i]
		if g.Delimiter != attributes.TagJobAttributes {
			continue
		}
		row := JobRow{State: 3}
		if v, ok := attributes.GroupInt(g, "job-id"); ok {
			row.ID = v
		}
		if v, ok := attributes.GroupInt(g, "job-state"); ok {
			row.State = v
		}
		row.Name = attributes.GroupFirstString(g, "job-name")
		row.User = attributes.GroupFirstString(g, "job-originating-user-name")
		row.Format = attributes.GroupFirstString(g, "document-format")
		out = append(out, row)
	}
	return out, nil
}

// Cancel aborts a job.
func (c *Client) Cancel(id int32) error {
	opAttrs := []attributes.Attribute{
		attributes.URI("printer-uri", c.URI),
		attributes.Integer("job-id", id),
	}
	if c.User != "" {
		opAttrs = append(opAttrs, attributes.Name("requesting-user-name", c.User))
	}
	msg, err := c.roundTrip(0x0008, opAttrs, nil, nil, false)
	if err != nil {
		return err
	}
	if msg.Code != StatusOK {
		return fmt.Errorf("client: Cancel-Job failed: %s (0x%04X)", statusMessage(msg), msg.Code)
	}
	return nil
}

// roundTrip encodes one IPP request, POSTs it, and decodes the response.
// gzipOutput (set only by Print-Job) compresses the document stream.
func (c *Client) roundTrip(op uint16, opAttrs, jobAttrs []attributes.Attribute, doc io.Reader, gzipOutput bool) (*attributes.Message, error) {
	var body bytes.Buffer
	body.Write([]byte{2, 0, byte(op >> 8), byte(op)}) // version 2.0
	_ = binary.Write(&body, binary.BigEndian, int32(1))
	body.WriteByte(attributes.TagOperationAttributes)
	if err := writeGroup(&body, opAttrs); err != nil {
		return nil, err
	}
	if len(jobAttrs) > 0 {
		body.WriteByte(attributes.TagJobAttributes)
		if err := writeGroup(&body, jobAttrs); err != nil {
			return nil, err
		}
	}
	body.WriteByte(attributes.TagEndOfAttributes)
	if doc != nil {
		sink := io.Writer(&body)
		var zw *gzip.Writer
		if gzipOutput {
			zw = gzip.NewWriter(&body)
			sink = zw
		}
		if _, err := io.Copy(sink, doc); err != nil {
			return nil, fmt.Errorf("client: reading document: %w", err)
		}
		// Close BEFORE the body is sent: the gzip trailer is part of
		// the request.
		if zw != nil {
			if err := zw.Close(); err != nil {
				return nil, fmt.Errorf("client: closing gzip stream: %w", err)
			}
		}
	}

	req, err := http.NewRequest(http.MethodPost, c.httpURL(), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/ipp")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client: POST %s: %w", c.httpURL(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client: HTTP %d from %s", resp.StatusCode, c.httpURL())
	}
	return attributes.DecodeMessageStream(resp.Body)
}

func writeGroup(w io.Writer, attrs []attributes.Attribute) error {
	// attributes-charset / attributes-natural-language lead every
	// operation group (RFC 8011 §4.1.4).
	full := append([]attributes.Attribute{
		attributes.Text(attributes.TagCharset, "attributes-charset", "utf-8"),
		attributes.Text(attributes.TagNaturalLang, "attributes-natural-language", "en-us"),
	}, attrs...)
	return attributes.WriteAttributes(w, full)
}

func statusMessage(msg *attributes.Message) string {
	s := attributes.GroupFirstString(msg.Operation(), "status-message")
	if s == "" {
		return "no status message"
	}
	return s
}
