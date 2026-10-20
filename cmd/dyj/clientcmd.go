package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/client"
	"github.com/232252/dyj/pkg/discovery"
	"github.com/232252/dyj/pkg/render"
)

// resolveTarget turns "first printer on the LAN" into a URI: an empty
// or mDNS-style target browses and picks the first _ipp._tcp hit.
func resolveTarget(target string, timeout time.Duration, ifaces string) (string, error) {
	if target != "" && !strings.HasPrefix(target, "_") {
		if strings.Contains(target, "://") {
			return target, nil
		}
		// Bare "host:port" from `dyj browse` output → canonical URI.
		return "ipp://" + target + "/ipp/print", nil
	}
	found, err := browseOnce(timeout, ifaces)
	if err != nil {
		return "", err
	}
	for _, f := range found {
		if f.Port == 0 {
			continue
		}
		// Multi-homed printers advertise every interface's address; the
		// mDNS answer order says nothing about routability from HERE.
		// Probe each candidate briefly and take the first that accepts
		// a TCP connection.
		for _, a := range reachableAddrs(f) {
			return fmt.Sprintf("ipp://%s:%d/ipp/print", a, f.Port), nil
		}
	}
	return "", errors.New("no IPP printer found on the LAN (service seen but no address answered)")
}

// reachableAddrs returns the printer's addresses ordered by a quick TCP
// probe (reachable first, then the rest as fallback).
func reachableAddrs(f discovery.Found) []string {
	addrs := f.Addrs
	if len(addrs) == 0 {
		addrs = []string{f.Host}
	}
	var ok, rest []string
	for _, a := range addrs {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(a, fmt.Sprint(f.Port)), 800*time.Millisecond)
		if err == nil {
			c.Close()
			ok = append(ok, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(ok, rest...)
}

func cmdBrowse(args []string) error {
	fs := flag.NewFlagSet("browse", flag.ContinueOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "how long to browse")
	ifaces := fs.String("iface", "", "interfaces to browse on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	found, err := browseOnce(*timeout, *ifaces)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		return errors.New("no _ipp._tcp printer found (is the server running?)")
	}
	for _, f := range found {
		addrs := f.Addrs
		if len(addrs) == 0 {
			addrs = []string{f.Host}
		}
		fmt.Printf("%s:%d  %q\n", strings.Join(addrs, ","), f.Port, f.Instance)
		if len(f.TXT) > 0 {
			fmt.Printf("    txt: %s\n", strings.Join(f.TXT, " "))
		}
	}
	return nil
}

func browseOnce(timeout time.Duration, ifaces string) ([]discovery.Found, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ifs, err := discovery.SelectInterfaces(splitList(ifaces))
	if err != nil {
		return nil, err
	}
	return discovery.Browse(ifs, timeout)
}

func cmdListInterfaces() error {
	ifs, err := discovery.MulticastCapableInterfaces()
	if err != nil {
		return err
	}
	for _, ifi := range ifs {
		addrs := discovery.InterfaceAddrs([]*net.Interface{ifi})
		strs := make([]string, len(addrs))
		for i, a := range addrs {
			strs[i] = a.String()
		}
		fmt.Printf("%-16s mtu=%-5d addrs=%s\n", ifi.Name, ifi.MTU, strings.Join(strs, ","))
	}
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	ifaces := fs.String("iface", "", "interfaces for mDNS fallback")
	user := fs.String("user", defaultUser(), "requesting-user-name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: dyj status <uri|host:port>")
	}
	uri, err := resolveTarget(fs.Arg(0), 5*time.Second, *ifaces)
	if err != nil {
		return err
	}
	c, err := client.ParseInsecure(uri, *user)
	if err != nil {
		return err
	}
	msg, err := c.GetPrinterAttributes()
	if err != nil {
		return err
	}
	if msg.Code != 0 {
		return fmt.Errorf("printer answered with status 0x%04X", msg.Code)
	}
	p := msg.Group(0x04) // printer-attributes group
	if p == nil {
		return errors.New("response carries no printer-attributes group")
	}
	get := func(n string) string { return attributes.GroupFirstString(p, n) }
	state, _ := attributes.GroupInt(p, "printer-state")
	accepting, _ := attributes.GroupBool(p, "printer-is-accepting-jobs")
	fmt.Printf("printer:   %s\n", get("printer-name"))
	fmt.Printf("state:     %s\n", stateName(state))
	fmt.Printf("accepting: %s\n", boolName(accepting, true))
	fmt.Printf("make:      %s\n", get("printer-make-and-model"))
	fmt.Printf("uuid:      %s\n", get("printer-uuid"))
	fmt.Printf("formats:   %s\n", strings.Join(attributes.GroupStrings(p, "document-format-supported"), ","))
	fmt.Printf("media:     %s\n", get("media-supported"))
	fmt.Printf("queued:    %s\n", get("queued-job-count"))
	return nil
}

// stateName renders printer/job state enums for humans. The two enums
// overlap in Go's type system (both int32), so printer-only values 6-9
// are the RFC 8011 §5.3.8 job states.
func stateName(v int32) string {
	switch v {
	case 3:
		return "idle/pending"
	case 4:
		return "processing/held"
	case 5:
		return "stopped/processing"
	case 6:
		return "stopped"
	case 7:
		return "canceled"
	case 8:
		return "aborted"
	case 9:
		return "completed"
	default:
		return fmt.Sprint(v)
	}
}

func boolName(b, ok bool) string {
	if !ok {
		return "?"
	}
	if b {
		return "yes"
	}
	return "no"
}

func cmdPrint(args []string) error {
	fs := flag.NewFlagSet("print", flag.ContinueOnError)
	user := fs.String("user", defaultUser(), "requesting-user-name")
	copies := fs.Int("copies", 1, "copies")
	media := fs.String("media", "", "PWG media name (e.g. iso_a4_210x297mm)")
	color := fs.Bool("color", false, "request color")
	jobName := fs.String("jobname", "", "job name (default: file name)")
	ifaces := fs.String("iface", "", "interfaces for mDNS fallback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return errors.New("usage: dyj print [-copies N] <uri|host:port> <file>")
	}
	uri, err := resolveTarget(fs.Arg(0), 5*time.Second, *ifaces)
	if err != nil {
		return err
	}
	file := fs.Arg(1)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	name := *jobName
	if name == "" {
		name = filepath.Base(file)
	}

	format, body, err := encodeForSubmission(file, data)
	if err != nil {
		return err
	}

	cl, err := client.ParseInsecure(uri, *user)
	if err != nil {
		return err
	}
	id, err := cl.PrintJob(bytes.NewReader(body), int64(len(body)), format, client.PrintOptions{
		JobName: name, Copies: *copies, Media: *media, Color: *color,
	})
	if err != nil {
		return err
	}
	fmt.Printf("dyj: submitted job %d (%s, %d bytes as %s) to %s\n", id, name, len(body), format, uri)
	return nil
}

// encodeForSubmission converts local files into a format the server
// declares: text → PWG raster, images → PWG raster, PDF passthrough
// (the server rasterizes it when it advertises application/pdf —
// checking what the server actually supports is `dyj status`'s job),
// PWG passthrough.
func encodeForSubmission(file string, data []byte) (format string, body []byte, err error) {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".pdf":
		// Sniff, don't trust the extension: a renamed text file should
		// fail honestly, not corrupt a print run.
		if len(data) >= 5 && string(data[:5]) == "%PDF-" {
			return "application/pdf", data, nil
		}
		return "", nil, fmt.Errorf("%s has .pdf extension but no %%PDF- magic", file)
	case ".txt", ".text", ".log", ".md":
		pages, err := render.TextPages(string(data), render.TextOptions{DPI: 300})
		if err != nil {
			return "", nil, err
		}
		return "image/pwg-raster", mustEncodePWG(pages), nil
	case ".jpg", ".jpeg", ".png":
		pages, err := render.ImagePages(data, render.ImageOptions{DPI: 300})
		if err != nil {
			return "", nil, err
		}
		return "image/pwg-raster", mustEncodePWG(pages), nil
	case ".pwg", ".ras":
		return "image/pwg-raster", data, nil
	default:
		return "", nil, fmt.Errorf("unsupported file type %q (want .txt .jpg .png .pdf .pwg)", filepath.Ext(file))
	}
}

func mustEncodePWG(pages []*render.Page) []byte {
	var buf bytes.Buffer
	w, err := render.NewWriter(&buf)
	if err != nil {
		panic(err)
	}
	for _, p := range pages {
		if err := w.WritePage(p); err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

func cmdJobs(args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ContinueOnError)
	user := fs.String("user", defaultUser(), "requesting-user-name")
	which := fs.String("which", "not-completed", "not-completed | completed | all")
	ifaces := fs.String("iface", "", "interfaces for mDNS fallback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: dyj jobs <uri|host:port>")
	}
	uri, err := resolveTarget(fs.Arg(0), 5*time.Second, *ifaces)
	if err != nil {
		return err
	}
	c, err := client.ParseInsecure(uri, *user)
	if err != nil {
		return err
	}
	rows, err := c.Jobs(*which)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("no jobs")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("job %-6d %-10s %-24s %-20s %s\n",
			r.ID, stateName(r.State), truncate(r.Name, 24), truncate(r.User, 20), r.Format)
	}
	return nil
}

func cmdCancel(args []string) error {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	user := fs.String("user", defaultUser(), "requesting-user-name")
	ifaces := fs.String("iface", "", "interfaces for mDNS fallback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return errors.New("usage: dyj cancel <uri|host:port> <job-id>")
	}
	uri, err := resolveTarget(fs.Arg(0), 5*time.Second, *ifaces)
	if err != nil {
		return err
	}
	var id int32
	if _, err := fmt.Sscan(fs.Arg(1), &id); err != nil {
		return fmt.Errorf("job-id %q is not a number", fs.Arg(1))
	}
	c, err := client.ParseInsecure(uri, *user)
	if err != nil {
		return err
	}
	if err := c.Cancel(id); err != nil {
		return err
	}
	fmt.Printf("dyj: canceled job %d\n", id)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func defaultUser() string {
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return os.Getenv("USER")
}
