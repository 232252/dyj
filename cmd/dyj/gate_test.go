package main

import (
	"io"
	"net/http"
	"testing"

	"github.com/232252/dyj/pkg/logx"
	"github.com/232252/dyj/pkg/pairing"
)

// TestGateLoopbackTokenAndTrustedIP pins the three ways through the
// data-plane gate (research/17): loopback, a valid token in the URI path,
// and the source IP of a device that paired in-app — the path the OS print
// stack takes, which carries no token at all.
func TestGateLoopbackTokenAndTrustedIP(t *testing.T) {
	store, err := pairing.Open("", pairing.Limits{}, logx.New(io.Discard, logx.LevelDebug))
	if err != nil {
		t.Fatal(err)
	}
	pin, _ := store.Current()
	token, err := store.Pair(pin, "dev1", "pad", "10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	n := &node{pair: store, log: logx.New(io.Discard, logx.LevelDebug)}
	g := n.gate()
	req := func(remote string) *http.Request {
		return &http.Request{RemoteAddr: remote}
	}

	if !g(req("127.0.0.1:55555"), "") {
		t.Fatal("loopback must pass")
	}
	if !g(req("[::1]:55555"), "") {
		t.Fatal("loopback v6 must pass")
	}
	if !g(req("10.0.0.9:4444"), "") {
		t.Fatal("paired device ip must pass tokenless")
	}
	if g(req("10.0.0.10:4444"), "") {
		t.Fatal("unknown ip without token must be denied")
	}
	if !g(req("10.0.0.10:4444"), token) {
		t.Fatal("valid token must pass from anywhere")
	}
	if g(req("10.0.0.10:4444"), "bogus") {
		t.Fatal("bogus token must be denied")
	}
	if !store.Revoke("dev1") {
		t.Fatal("revoke dev1")
	}
	if g(req("10.0.0.9:4444"), "") {
		t.Fatal("revoked device ip must be denied")
	}
}
