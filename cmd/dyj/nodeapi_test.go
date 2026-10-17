package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/232252/dyj/pkg/attributes"
	"github.com/232252/dyj/pkg/discovery"
)

func TestScanEntriesMergesPerInterfaceViews(t *testing.T) {
	// research/15 D-1: the raw and reference-client browsing paths report the
	// same printer under different ${IFACE} SRV targets; the merge key must
	// not include the host or one printer shows up as N rows.
	found := []discovery.Found{
		{
			Instance: "dyj-el._ipp._tcp.local",
			Host:     "dyj-el-br-e57f74e0bde4.local",
			Port:     6631,
			Addrs:    []string{"10.2.3.29", "172.17.0.1", "192.168.110.214"},
			TXTMap:   map[string]string{"ty": "dyj-el", "dyj": "1", "pair": "1", "rp": "ipp/print", "UUID": "u1"},
		},
		{
			Instance: "dyj-el._ipp._tcp.local.",
			Host:     "dyj-el-optun.local",
			Port:     6631,
			Addrs:    []string{"192.168.110.214"},
			TXTMap:   map[string]string{"ty": "dyj-el", "rp": "ipp/print", "UUID": "u1"},
		},
	}
	got := scanEntries(found, map[string]bool{})
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (merged): %+v", len(got), got)
	}
	e := got[0]
	if e.Name != "dyj-el" || !e.IsDyj || !e.NeedPIN || e.Path != "/ipp/print" {
		t.Errorf("unexpected fields: %+v", e)
	}
	if want := []string{"10.2.3.29", "172.17.0.1", "192.168.110.214"}; !reflect.DeepEqual(e.Addrs, want) {
		t.Errorf("addrs not unioned: got %v want %v", e.Addrs, want)
	}
}

func TestScanEntriesKeepsDistinctPrinters(t *testing.T) {
	found := []discovery.Found{
		{
			Instance: "dyj-a._ipp._tcp.local.", Host: "dyj-a.local.", Port: 6631,
			Addrs: []string{"192.168.110.10"}, TXTMap: map[string]string{"ty": "A"},
		},
		{
			Instance: "dyj-b._ipp._tcp.local.", Host: "dyj-b.local.", Port: 6631,
			Addrs: []string{"192.168.110.11"}, TXTMap: map[string]string{"ty": "B"},
		},
		// same label, different port: a different service, must not merge
		{
			Instance: "dyj-a._ipp._tcp.local.", Host: "dyj-a.local.", Port: 6642,
			Addrs: []string{"192.168.110.10"}, TXTMap: map[string]string{"ty": "A-tls"},
		},
	}
	got := scanEntries(found, map[string]bool{})
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
}

func TestScanEntriesDropsOwnAndNonIPP(t *testing.T) {
	found := []discovery.Found{
		{Instance: "mine._ipp._tcp.local.", Host: "mine.local.", Port: 6631,
			TXTMap: map[string]string{"UUID": "own-uuid"}},
		{Instance: "_services._dns-sd._udp.local.", Host: "", Port: 0,
			TXTMap: map[string]string{}},
	}
	own := map[string]bool{"own-uuid": true, "mine.local": true}
	if got := scanEntries(found, own); len(got) != 0 {
		t.Fatalf("got %d entries, want 0: %+v", len(got), got)
	}
}

func TestUnionAddrs(t *testing.T) {
	got := unionAddrs([]string{"a", "b"}, []string{"b", "", "c"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestPinKnownDedupesAndPrepends(t *testing.T) {
	scanned := []scanEntry{
		{Name: "other", Host: "other.local", Port: 6631, Addrs: []string{"192.168.1.9"}},
		{Name: "el", Host: "el.local", Port: 6631, Addrs: []string{"192.168.110.214", "10.2.3.29"}, IsDyj: true, NeedPIN: true},
	}
	known := []scanEntry{
		// same printer, reached by an address the browse already listed
		{Name: "el-saved", Host: "192.168.110.214", Port: 6631, Reachable: true, Known: true},
		// mesh address the browse cannot see
		{Name: "mesh", Host: "10.126.126.1", Port: 6631, Reachable: true, Known: true, IsDyj: true},
		// different port must not collapse into the LAN row
		{Name: "el-tls", Host: "192.168.110.214", Port: 6632, Reachable: true, Known: true},
	}
	got := pinKnown(scanned, known)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4: %+v", len(got), got)
	}
	if got[0].Name != "el" || !got[0].Known || !got[0].NeedPIN {
		t.Errorf("merged row lost browse fields: %+v", got[0])
	}
	if got[1].Name != "mesh" || !got[1].Known || !got[1].Reachable {
		t.Errorf("mesh row = %+v", got[1])
	}
	if got[2].Port != 6632 || !got[2].Known {
		t.Errorf("distinct port not kept: %+v", got[2])
	}
	if got[3].Name != "other" || got[3].Known {
		t.Errorf("unrelated row = %+v", got[3])
	}
}

func TestPinKnownEmptyIsIdentity(t *testing.T) {
	in := []scanEntry{{Name: "a", Port: 6631}}
	got := pinKnown(in, nil)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyAdded(t *testing.T) {
	cases := []struct {
		name       string
		queueOK    bool
		queueKnown bool
		probed     bool
		pr         ippProbe
		want       string
	}{
		{"queue gone", false, true, true, ippProbe{Alive: true}, "missing"},
		{"no address stored", true, true, false, ippProbe{}, "ok"},
		{"ipp silent", true, true, true, ippProbe{}, "offline"},
		{"token revoked", true, true, true, ippProbe{Alive: true, Revoked: true, Status: 401}, "revoked"},
		{"ipp ok", true, true, true, ippProbe{Alive: true, Name: "el"}, "ok"},
		{"queue query failed stays inconclusive", false, false, false, ippProbe{}, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyAdded(tc.queueOK, tc.queueKnown, tc.probed, tc.pr); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestProbeIPPAliveRevokedAndSilent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		writeTestIPP(w, "office")
	})
	mux.HandleFunc("/p/dead/ipp/print", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	mux.HandleFunc("/silent", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(2 * time.Second) // peer accepted TCP and then said nothing
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)

	ok := probeIPP(host, port, "/ipp/print", time.Second)
	if !ok.Alive || ok.Revoked || ok.Name != "office" {
		t.Errorf("live probe = %+v", ok)
	}
	rev := probeIPP(host, port, "/p/dead/ipp/print", time.Second)
	if !rev.Alive || !rev.Revoked || rev.Status != http.StatusUnauthorized {
		t.Errorf("revoked probe = %+v", rev)
	}
	silent := probeIPP(host, port, "/silent", 200*time.Millisecond)
	if silent.Alive {
		t.Errorf("silent handshake must not count as alive: %+v", silent)
	}
}

func splitTestHost(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u := rawURL
	u = u[len("http://"):]
	host, portStr, ok := cutHostPort(u)
	if !ok {
		t.Fatalf("bad test url %q", rawURL)
	}
	var port int
	for _, r := range portStr {
		port = port*10 + int(r-'0')
	}
	return host, port
}

func cutHostPort(hostport string) (string, string, bool) {
	i := len(hostport) - 1
	for i >= 0 && hostport[i] != ':' {
		i--
	}
	if i <= 0 {
		return "", "", false
	}
	return hostport[:i], hostport[i+1:], true
}

func writeTestIPP(w http.ResponseWriter, name string) {
	var b bytes.Buffer
	b.Write([]byte{2, 0, 0, 0})
	_ = binary.Write(&b, binary.BigEndian, int32(1))
	b.WriteByte(attributes.TagOperationAttributes)
	_ = attributes.WriteAttributes(&b, []attributes.Attribute{
		attributes.Text(attributes.TagCharset, "attributes-charset", "utf-8"),
		attributes.Text(attributes.TagNaturalLang, "attributes-natural-language", "en-us"),
	})
	b.WriteByte(attributes.TagPrinterAttributes)
	_ = attributes.WriteAttributes(&b, []attributes.Attribute{
		attributes.Name("printer-name", name),
	})
	b.WriteByte(attributes.TagEndOfAttributes)
	w.Header().Set("Content-Type", "application/ipp")
	_, _ = w.Write(b.Bytes())
}
