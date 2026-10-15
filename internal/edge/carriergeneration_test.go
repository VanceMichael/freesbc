package edge

// End-to-end tests for carrier address generations: when DNS moves a
// carrier to a new target, NEW transactions leave from the new snapshot, a
// live call keeps using its creation-time connection, and after that
// connection is lost the call's in-dialog traffic redials the OLD remote
// with the same carrier's settings. The last related dialog to end reclaims
// the retired route. Two real stream gateways stand in for the two
// generations on loopback; the carrier resolves through SRV so the target
// port can change while its name stays the same.

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

const genCarrierHost = "gw.alpha.test"

// genRig is one edge with carrier "alpha" resolving through SRV, with a
// gateway per DNS generation.
type genRig struct {
	h     *harness
	fs    *fakeSwitch
	stub  *dnsStub
	oldGW *streamCarrier
	newGW *streamCarrier
}

func startGenerationRig(t *testing.T) *genRig {
	t.Helper()
	oldGW := startStreamCarrier(t, "tcp", nil, insecureClient())
	newGW := startStreamCarrier(t, "tcp", nil, insecureClient())

	pubUDP, priv, up, cp := freePort(t), freePort(t), freePort(t), freePort(t)
	mediaBase := nextMediaBase(t)
	entry := fmt.Sprintf("alpha: {host: %q, transport: tcp}", genCarrierHost)
	yaml := harnessYAML(t, []string{fmt.Sprintf("127.0.0.1:%d", up)}, pubUDP, 0, 0, mediaBase, "")
	yaml = strings.Replace(yaml, "edge:\n",
		fmt.Sprintf("edge:\n  switch_carrier_port: %d\n  carriers:\n    %s\n", cp, entry), 1)
	h := newHarness(t, yaml, priv)
	h.upstream = fmt.Sprintf("127.0.0.1:%d", up)
	fs := startFakeSwitch(t, h.upstream)
	cs := startFakeSwitch(t, fmt.Sprintf("127.0.0.1:%d", cp))
	h.upstreams["carrier-port"] = cs

	stub := &dnsStub{
		srv: map[string][]*net.SRV{genCarrierHost: {
			{Target: genCarrierHost + ".", Port: uint16(oldGW.port), Priority: 0, Weight: 1},
		}},
		ips: map[string][]string{genCarrierHost: {"127.0.0.1"}},
	}
	h.srv.carriers.lookupSRV, h.srv.carriers.lookupIP = stub.lookupSRV, stub.lookupIP
	h.run()

	rig := &genRig{h: h, fs: fs, stub: stub, oldGW: oldGW, newGW: newGW}
	waitFor(t, 5*time.Second, "the first generation to resolve", func() bool {
		dest, ok := h.srv.carrierDest("alpha")
		return ok && dest == fmt.Sprintf("127.0.0.1:%d", oldGW.port)
	})
	return rig
}

func startTLSGenerationRig(t *testing.T) *genRig {
	t.Helper()
	pki := newTestPKI(t)
	leaf := pki.issue(false, []string{genCarrierHost})
	serverConf := tlsServerConf(leaf)

	oldGW := startStreamCarrier(t, "tls", serverConf, insecureClient())
	newGW := startStreamCarrier(t, "tls", serverConf, insecureClient())

	pubUDP, priv, up, cp := freePort(t), freePort(t), freePort(t), freePort(t)
	mediaBase := nextMediaBase(t)
	entry := fmt.Sprintf("alpha: {host: %q, transport: tls, ca_file: %q}", genCarrierHost, pki.caFile())
	yaml := harnessYAML(t, []string{fmt.Sprintf("127.0.0.1:%d", up)}, pubUDP, 0, 0, mediaBase, "")
	yaml = strings.Replace(yaml, "edge:\n",
		fmt.Sprintf("edge:\n  switch_carrier_port: %d\n  carriers:\n    %s\n", cp, entry), 1)
	h := newHarness(t, yaml, priv)
	h.upstream = fmt.Sprintf("127.0.0.1:%d", up)
	fs := startFakeSwitch(t, h.upstream)
	cs := startFakeSwitch(t, fmt.Sprintf("127.0.0.1:%d", cp))
	h.upstreams["carrier-port"] = cs

	stub := &dnsStub{
		srv: map[string][]*net.SRV{genCarrierHost: {
			{Target: genCarrierHost + ".", Port: uint16(oldGW.port), Priority: 0, Weight: 1},
		}},
		ips: map[string][]string{genCarrierHost: {"127.0.0.1"}},
	}
	h.srv.carriers.lookupSRV, h.srv.carriers.lookupIP = stub.lookupSRV, stub.lookupIP
	h.run()

	rig := &genRig{h: h, fs: fs, stub: stub, oldGW: oldGW, newGW: newGW}
	waitFor(t, 5*time.Second, "the first generation to resolve", func() bool {
		dest, ok := h.srv.carrierDest("alpha")
		return ok && dest == fmt.Sprintf("127.0.0.1:%d", oldGW.port)
	})
	return rig
}

func (g *genRig) carrierURI() sip.Uri {
	return sip.Uri{User: "+442071234567", Host: genCarrierHost, Port: 5060}
}

// place has the switch place a call that the given gateway answers.
func (g *genRig) place(t *testing.T, gw *streamCarrier) *sip.Response {
	t.Helper()
	res := g.fs.call(t, g.carrierURI(), g.h.privateSIP, phoneOfferSDP(g.fs.rtpPort))
	if res.StatusCode != 200 {
		t.Fatalf("INVITE to the carrier: got %d, want 200", res.StatusCode)
	}
	gw.waitInbound(t, sip.INVITE)
	g.fs.sendAckTo2xx(t, res)
	gw.waitInbound(t, sip.ACK)
	return res
}

// flip moves the SRV target to gw and refreshes the directory.
func (g *genRig) flip(t *testing.T, gw *streamCarrier) {
	t.Helper()
	g.stub.mu.Lock()
	g.stub.srv[genCarrierHost] = []*net.SRV{
		{Target: genCarrierHost + ".", Port: uint16(gw.port), Priority: 0, Weight: 1},
	}
	g.stub.mu.Unlock()
	d := g.h.srv.carriers
	d.mu.Lock()
	e := d.entries["alpha"]
	if e == nil {
		d.mu.Unlock()
		t.Fatal("the carrier has no cache entry to expire")
	}
	e.expiry = time.Now().Add(-time.Second)
	d.mu.Unlock()
	d.refresh(context.Background())
	want := fmt.Sprintf("127.0.0.1:%d", gw.port)
	if dest, ok := g.h.srv.carrierDest("alpha"); !ok || dest != want {
		t.Fatalf("after the refresh carrierDest = %q,%v, want %s", dest, ok, want)
	}
}

// awaitPoolMiss waits until sipgo's pool no longer holds a connection to
// dest over network, as TestCarrierConnectionRedialAndShutdown does.
func (g *genRig) awaitPoolMiss(t *testing.T, network, dest string) {
	t.Helper()
	waitFor(t, 5*time.Second, "the old connection leaving the pool", func() bool {
		c, err := g.h.srv.srv.TransportLayer().GetConnection(network, dest)
		if err == nil && c != nil {
			_, _ = c.TryClose()
			return false
		}
		return true
	})
}

// A refresh splits traffic by generation: the new call takes the new
// snapshot, both calls' in-dialog traffic keeps each one's own connection,
// and the retired routes are reclaimed once both calls end.
func TestCarrierGenerationNewCallUsesSnapshotOldCallKeepsRoute(t *testing.T) {
	rig := startGenerationRig(t)
	res1 := rig.place(t, rig.oldGW)

	rig.flip(t, rig.newGW)
	res2 := rig.place(t, rig.newGW)
	rig.oldGW.noInbound(t) // the new transaction never fell back to the old target

	if bye := rig.fs.uacBye(t, res1); bye.StatusCode != 200 {
		t.Fatalf("BYE on the old generation: got %d, want 200", bye.StatusCode)
	}
	rig.oldGW.waitInbound(t, sip.BYE)
	if n := rig.oldGW.ln.accepted(); n != 1 {
		t.Errorf("old gateway accepted %d connections, want the one reused for the call and its BYE", n)
	}
	if bye := rig.fs.uacBye(t, res2); bye.StatusCode != 200 {
		t.Fatalf("BYE on the new generation: got %d, want 200", bye.StatusCode)
	}
	rig.newGW.waitInbound(t, sip.BYE)
	if n := rig.newGW.ln.accepted(); n != 1 {
		t.Errorf("new gateway accepted %d connections, want one", n)
	}
	waitForRelease(t, rig.h)
	if n := rig.h.srv.carriers.pinCount(); n != 0 {
		t.Errorf("%d retired routes remain after both dialogs ended", n)
	}
}

// With the creation-time connection gone, the old dialog redials the OLD
// remote with its own carrier identity, while the new-generation call is
// untouched. Runs over TCP and TLS, so the redial keeps the pinned
// carrier's SNI/CA/client certificate configuration.
func TestCarrierGenerationRedialsOldRemoteAfterFlowLoss(t *testing.T) {
	for _, transport := range []string{"tcp", "tls"} {
		t.Run(transport, func(t *testing.T) {
			var rig *genRig
			if transport == "tls" {
				rig = startTLSGenerationRig(t)
			} else {
				rig = startGenerationRig(t)
			}
			res1 := rig.place(t, rig.oldGW)

			rig.flip(t, rig.newGW)
			res2 := rig.place(t, rig.newGW)
			rig.oldGW.noInbound(t)

			oldDest := fmt.Sprintf("127.0.0.1:%d", rig.oldGW.port)
			rig.oldGW.ln.closeAccepted()
			rig.awaitPoolMiss(t, transport, oldDest)

			if bye := rig.fs.uacBye(t, res1); bye.StatusCode != 200 {
				t.Fatalf("BYE after the old flow was lost: got %d, want 200", bye.StatusCode)
			}
			rig.oldGW.waitInbound(t, sip.BYE)
			if n := rig.oldGW.ln.accepted(); n != 2 {
				t.Errorf("old gateway accepted %d connections, want a second one for the redial", n)
			}

			if bye := rig.fs.uacBye(t, res2); bye.StatusCode != 200 {
				t.Fatalf("BYE on the current generation: got %d, want 200", bye.StatusCode)
			}
			rig.newGW.waitInbound(t, sip.BYE)
			if n := rig.newGW.ln.accepted(); n != 1 {
				t.Errorf("new gateway accepted %d connections, want one", n)
			}
			waitForRelease(t, rig.h)
			if n := rig.h.srv.carriers.pinCount(); n != 0 {
				t.Errorf("%d retired routes remain after both dialogs ended", n)
			}
		})
	}
}

// A CANCEL for a still-early call follows its creation-time generation: the
// snapshot moved and the original connection was lost, so the CANCEL
// redials the old remote and the switch gets its 487.
func TestCarrierGenerationCancelRedialsOldRemote(t *testing.T) {
	rig := startGenerationRig(t)
	rig.oldGW.setAnswer(func(req *sip.Request, tx sip.ServerTransaction) {
		switch req.Method {
		case sip.INVITE:
			_ = tx.Respond(sip.NewResponseFromRequest(req, 100, "Trying", nil))
		default: // CANCEL and everything else are answered
			_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
		}
	})

	req, final := rig.fs.callAsync(t, rig.carrierURI(), rig.h.privateSIP, phoneOfferSDP(rig.fs.rtpPort))
	rig.oldGW.waitInbound(t, sip.INVITE)

	rig.flip(t, rig.newGW)
	rig.oldGW.ln.closeAccepted()
	rig.awaitPoolMiss(t, "tcp", fmt.Sprintf("127.0.0.1:%d", rig.oldGW.port))

	cancelReq := buildCancelFor(req)
	cancelReq.SetTransport("UDP")
	cancelReq.SetDestination(rig.h.privateSIP)
	cancelReq.Laddr = req.Laddr
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	tx, err := rig.fs.cli.TransactionRequest(ctx, cancelReq)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Terminate()
	select {
	case res := <-tx.Responses():
		if res.StatusCode != 200 {
			t.Fatalf("CANCEL: got %d, want 200", res.StatusCode)
		}
	case <-ctx.Done():
		t.Fatal("the CANCEL got no response")
	}
	select {
	case res := <-final:
		if res == nil || res.StatusCode != 487 {
			t.Fatalf("INVITE final after CANCEL = %v, want 487", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no final response after CANCEL")
	}
	rig.oldGW.waitInbound(t, sip.CANCEL)
	if n := rig.oldGW.ln.accepted(); n != 2 {
		t.Errorf("old gateway accepted %d connections, want the CANCEL redialed onto a second one", n)
	}
	waitForRelease(t, rig.h)
	if n := rig.h.srv.carriers.pinCount(); n != 0 {
		t.Errorf("%d retired routes remain after the early dialog ended", n)
	}
}
