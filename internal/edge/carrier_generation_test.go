package edge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	fsip "github.com/freesbc/freesbc/internal/sip"
)

// This file proves the carrier generation contract over real loopback
// TLS carriers (carrierpin.go):
//
//   - after a DNS refresh a NEW transaction is selected from the new
//     snapshot only, while an existing dialog's requests stay on its
//     creation-time carrier identity, transport and remote;
//   - once the old connection is gone, ACK/UPDATE/BYE and an early-dialog
//     CANCEL re-dial the OLD remote with the SAME carrier TLS/SNI/CA;
//   - the retired route is reclaimed when its last dialog ends, and the old
//     address can neither be dialed for a new transaction nor borrow another
//     carrier's identity;
//   - one address moving between two carriers keeps each dialog's TLS
//     attribution.
//
// The carriers resolve through SRV, so a refresh can move the gateway port
// (RFC 3263): the carrier host is a name, every SRV target is 127.0.0.1.

const genCarrierHost = "sip.carrier.test"

// genGateway is one fake carrier gateway with its port and observed SNIs.
type genGateway struct {
	sc   *streamCarrier
	addr string
}

// genRig is an edge with one (or two) TLS carrier(s) resolved through a
// controllable DNS stub.
type genRig struct {
	*harness
	fs   *fakeSwitch
	stub *dnsStub
}

func genSRV(port int) []*net.SRV {
	return []*net.SRV{{Target: "gw.carrier.test.", Port: uint16(port), Priority: 10, Weight: 1}}
}

// genLeaf is issued for every name a generation-test carrier can be called.
func genLeaf(t *testing.T, pki *testPKI) leaf {
	t.Helper()
	return pki.issue(false, []string{genCarrierHost, "sip.alpha.test", "sip.beta.test"})
}

func startGenGateway(t *testing.T, pki *testPKI) *genGateway {
	t.Helper()
	sc := startStreamCarrier(t, "tls", tlsServerConf(genLeaf(t, pki)), insecureClient())
	return &genGateway{sc: sc, addr: fmt.Sprintf("127.0.0.1:%d", sc.port)}
}

// genCarrierYAML renders the carriers block for the given carrier host
// names (all tls, the same test CA).
func genCarrierYAML(names []string, caFile string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "    %s: {host: %q, transport: tls, ca_file: %q}\n", n, n, caFile)
	}
	return "edge:\n  carriers:\n" + b.String()
}

// startGenRig builds the rig with the given carrier hosts and initial SRV
// map.
func startGenRig(t *testing.T, names []string, srv map[string][]*net.SRV, pki *testPKI) *genRig {
	t.Helper()
	pubUDP, priv, up := freePort(t), freePort(t), freePort(t)
	mediaBase := nextMediaBase(t)
	yaml := harnessYAML(t, []string{fmt.Sprintf("127.0.0.1:%d", up)}, pubUDP, 0, 0, mediaBase, "")
	block := genCarrierYAML(names, pki.caFile())
	yaml = strings.Replace(yaml, "edge:\n", block, 1)
	h := newHarness(t, yaml, priv)
	h.upstream = fmt.Sprintf("127.0.0.1:%d", up)
	h.fs = startFakeSwitch(t, h.upstream)
	stub := &dnsStub{srv: srv, ips: map[string][]string{"gw.carrier.test": {"127.0.0.1"}}}
	h.srv.carriers.lookupSRV, h.srv.carriers.lookupIP = stub.lookupSRV, stub.lookupIP
	h.run()
	for _, n := range names {
		waitFor(t, 5*time.Second, "carrier "+n+" to resolve", func() bool {
			_, ok := h.srv.carrierDest(n)
			return ok
		})
	}
	return &genRig{harness: h, fs: h.fs, stub: stub}
}

// refreshCarrierDNS expires the directory cache, mutates the stub and
// refreshes synchronously: the test's deterministic DNS refresh.
func (r *genRig) refreshCarrierDNS(t *testing.T, mutate func()) {
	t.Helper()
	d := r.srv.carriers
	d.mu.Lock()
	for _, e := range d.entries {
		e.expiry = time.Time{}
	}
	d.mu.Unlock()
	mutate()
	d.refresh(context.Background())
}

func (r *genRig) waitDest(t *testing.T, carrier, addr string) {
	t.Helper()
	waitFor(t, 5*time.Second, fmt.Sprintf("carrier %s at %s", carrier, addr), func() bool {
		dest, ok := r.srv.carrierDest(carrier)
		return ok && dest == addr
	})
}

// callCarrier has the switch place a call to a carrier host and complete
// it, returning the 200 and the INVITE the gateway saw.
func (r *genRig) callCarrier(t *testing.T, gw *genGateway, host string) *sip.Response {
	t.Helper()
	ruri := sip.Uri{User: "+442071234567", Host: host, Port: 5060}
	res := r.fs.call(t, ruri, r.privateSIP, phoneOfferSDP(r.fs.rtpPort))
	if res.StatusCode != 200 {
		t.Fatalf("switch INVITE to the carrier: got %d, want 200", res.StatusCode)
	}
	gw.sc.waitInbound(t, sip.INVITE)
	r.fs.sendAckTo2xx(t, res)
	gw.sc.waitInbound(t, sip.ACK)
	return res
}

// uacRequest sends an in-dialog non-INVITE request from the switch (the
// caller of the call res is the 200 of), like uacBye for BYE.
func (r *genRig) uacRequest(t *testing.T, method sip.RequestMethod, res *sip.Response) *sip.Response {
	t.Helper()
	f := r.fs
	req := sip.NewRequest(method, res.To().Address)
	sip.CopyHeaders("From", res, req)
	sip.CopyHeaders("To", res, req)
	sip.CopyHeaders("Call-ID", res, req)
	seq := uint32(1)
	if c := res.CSeq(); c != nil {
		seq = c.SeqNo
	}
	req.AppendHeader(&sip.CSeqHeader{SeqNo: seq + 1, MethodName: method})
	mf := sip.MaxForwardsHeader(70)
	req.AppendHeader(&mf)
	req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "3003", Host: hostOf(f.addr), Port: portOf(f.addr)}})
	copyRouteFromRecordRoute(res, req)
	via := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP",
		Host: hostOf(f.addr), Port: portOf(f.addr), Params: sip.NewParams()}
	via.Params.Add("branch", sip.GenerateBranchN(16))
	req.PrependHeader(via)
	req.SetTransport("UDP")
	req.SetDestination(f.inDialogDest(t, req, res))
	req.Laddr = sip.Addr{IP: net.ParseIP(hostOf(f.addr)), Port: portOf(f.addr)}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := f.cli.TransactionRequest(ctx, req)
	if err != nil {
		t.Fatalf("fake switch %s: %v", method, err)
	}
	defer tx.Terminate()
	for {
		select {
		case rr, ok := <-tx.Responses():
			if !ok {
				t.Fatalf("fake switch %s: no final response", method)
			}
			if rr.StatusCode >= 200 {
				return rr
			}
		case <-tx.Done():
			t.Fatalf("fake switch %s: %v", method, tx.Err())
		case <-ctx.Done():
			t.Fatalf("fake switch %s timed out", method)
		}
	}
}

// fsCancel cancels an INVITE the switch placed with callAsync.
func (r *genRig) fsCancel(t *testing.T, req *sip.Request) *sip.Response {
	t.Helper()
	cancel := fsip.BuildCancel(req)
	ctx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	tx, err := r.fs.cli.TransactionRequest(ctx, cancel)
	if err != nil {
		t.Fatalf("fake switch CANCEL: %v", err)
	}
	defer tx.Terminate()
	for {
		select {
		case res, ok := <-tx.Responses():
			if !ok {
				t.Fatal("fake switch CANCEL: no final response")
			}
			if res.StatusCode >= 200 {
				return res
			}
		case <-tx.Done():
			t.Fatalf("fake switch CANCEL: %v", tx.Err())
		case <-ctx.Done():
			t.Fatal("fake switch CANCEL timed out")
		}
	}
}

// waitConnPooledGone waits until sipgo's pool holds no connection to addr.
func (r *genRig) waitConnPooledGone(t *testing.T, addr string) {
	t.Helper()
	waitFor(t, 5*time.Second, "the pooled connection to "+addr+" to go", func() bool {
		c, err := r.srv.srv.TransportLayer().GetConnection("tls", addr)
		if err == nil && c != nil {
			_, _ = c.TryClose()
			return false
		}
		return true
	})
}

// probeOldDial asks requireFlow whether it would serve a fresh request to a
// retired address: with no pinned dialog it must refuse (errNoFlow).
func (r *genRig) probeOldDial(t *testing.T, addr string) error {
	t.Helper()
	req := sip.NewRequest(sip.OPTIONS, sip.Uri{Host: "probe.invalid", Port: 1})
	req.SetTransport("TLS")
	req.SetDestination(addr)
	return r.srv.requireFlow(context.Background(), req)
}

// After a successful refresh, NEW transactions take the new snapshot only;
// the established dialog keeps its first remote, reusing its connection;
// and the retired state is reclaimed with the dialogs.
func TestCarrierGenerationNewTransactionsUseSnapshotOldDialogKeepsRoute(t *testing.T) {
	pki := newTestPKI(t)
	gw1 := startGenGateway(t, pki)
	r := startGenRig(t, []string{genCarrierHost},
		map[string][]*net.SRV{genCarrierHost: genSRV(gw1.sc.port)}, pki)
	gw2 := startGenGateway(t, pki)

	res1 := r.callCarrier(t, gw1, genCarrierHost)
	if n := gw1.sc.ln.accepted(); n != 1 {
		t.Fatalf("gateway 1 accepted %d connections, want 1", n)
	}

	// Refresh moves the carrier onto gateway 2.
	r.refreshCarrierDNS(t, func() {
		r.stub.srv[genCarrierHost] = genSRV(gw2.sc.port)
	})
	r.waitDest(t, genCarrierHost, gw2.addr)
	// Both gateways are 127.0.0.1 in the lab, so the source set keeps the
	// IP; the per IP:port admission — what a new dial is selected from — is
	// what must have moved, and the old dialog keeps its own pin.
	if name, ok := r.srv.carrierTarget("tls", gw1.addr); ok {
		t.Errorf("the retired address still resolves to %q for a new dial", name)
	}
	if n := r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()); n != 1 {
		t.Errorf("retiredCount = %d, want 1", n)
	}

	// A mid-dialog UPDATE on the old call rides the original connection.
	if res := r.uacRequest(t, sip.UPDATE, res1); res.StatusCode != 200 {
		t.Fatalf("in-dialog UPDATE: got %d, want 200", res.StatusCode)
	}
	gw1.sc.waitInbound(t, sip.UPDATE)
	if n := gw1.sc.ln.accepted(); n != 1 {
		t.Errorf("gateway 1 accepted %d connections, want the old one reused", n)
	}

	// A NEW call is selected from the new snapshot.
	res2 := r.callCarrier(t, gw2, genCarrierHost)
	if n := gw2.sc.ln.accepted(); n != 1 {
		t.Errorf("gateway 2 accepted %d connections, want the new call", n)
	}

	// Each BYE goes to its own dialog's gateway, over the reused connection.
	if bye := r.fs.uacBye(t, res1); bye.StatusCode != 200 {
		t.Fatalf("BYE of the old call: got %d, want 200", bye.StatusCode)
	}
	gw1.sc.waitInbound(t, sip.BYE)
	if n := gw1.sc.ln.accepted(); n != 1 {
		t.Errorf("gateway 1 accepted %d connections, want the old one kept", n)
	}
	if bye := r.fs.uacBye(t, res2); bye.StatusCode != 200 {
		t.Fatalf("BYE of the new call: got %d, want 200", bye.StatusCode)
	}
	gw2.sc.waitInbound(t, sip.BYE)
	waitForRelease(t, r.harness)
	if n := r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()); n != 0 {
		t.Errorf("retiredCount after both calls ended = %d, want 0", n)
	}
}

// With the old connection lost, an in-dialog UPDATE and BYE re-dial the OLD
// remote with the pinned carrier's TLS (SNI/CA), and after the dialog ends
// the retired address refuses a fresh dial.
func TestCarrierGenerationRedialAfterFlowLost(t *testing.T) {
	pki := newTestPKI(t)
	gw1 := startGenGateway(t, pki)
	r := startGenRig(t, []string{genCarrierHost},
		map[string][]*net.SRV{genCarrierHost: genSRV(gw1.sc.port)}, pki)
	gw2 := startGenGateway(t, pki)

	res1 := r.callCarrier(t, gw1, genCarrierHost)
	r.refreshCarrierDNS(t, func() {
		r.stub.srv[genCarrierHost] = genSRV(gw2.sc.port)
	})
	r.waitDest(t, genCarrierHost, gw2.addr)
	if n := r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()); n != 1 {
		t.Fatalf("retiredCount = %d, want 1", n)
	}

	// The carrier drops the old connection; FreeSBC must notice and pool
	// nothing for the old remote.
	gw1.sc.ln.closeAccepted()
	r.waitConnPooledGone(t, gw1.addr)

	// A new call has already moved to gateway 2; the old dialog's UPDATE
	// re-dials gateway 1 as alpha.
	res2 := r.callCarrier(t, gw2, genCarrierHost)
	if res := r.uacRequest(t, sip.UPDATE, res1); res.StatusCode != 200 {
		t.Fatalf("UPDATE after the flow was lost: got %d, want 200", res.StatusCode)
	}
	gw1.sc.waitInbound(t, sip.UPDATE)
	if n := gw1.sc.ln.accepted(); n != 2 {
		t.Errorf("gateway 1 accepted %d connections, want a redial after the loss", n)
	}
	if snis := gw1.sc.snisObserved(); len(snis) != 2 || snis[0] != genCarrierHost || snis[1] != genCarrierHost {
		t.Errorf("gateway 1 SNIs = %v, want two %q handshakes", snis, genCarrierHost)
	}

	// The BYE reuses the redialed connection (no third one).
	if bye := r.fs.uacBye(t, res1); bye.StatusCode != 200 {
		t.Fatalf("BYE after the flow was lost: got %d, want 200", bye.StatusCode)
	}
	gw1.sc.waitInbound(t, sip.BYE)
	if n := gw1.sc.ln.accepted(); n != 2 {
		t.Errorf("gateway 1 accepted %d connections, want the redial reused", n)
	}
	// Only the old call ends here; the new one (gateway 2) stays up.
	waitFor(t, 5*time.Second, "the old call to end", func() bool {
		return r.srv.dialogs.sessionCount() == 1
	})
	if n := r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()); n != 0 {
		t.Errorf("retiredCount after the old call ended = %d, want 0", n)
	}
	// The retired connection is closed with the last pin; its pool entry
	// leaves on the read loop, so wait before probing.
	r.waitConnPooledGone(t, gw1.addr)
	if err := r.probeOldDial(t, gw1.addr); !errors.Is(err, errNoFlow) {
		t.Errorf("a fresh request to the retired address: err = %v, want errNoFlow", err)
	}
	if bye := r.fs.uacBye(t, res2); bye.StatusCode != 200 {
		t.Fatalf("BYE of the new call: got %d, want 200", bye.StatusCode)
	}
	gw2.sc.waitInbound(t, sip.BYE)
	waitForRelease(t, r.harness)
}

// An EARLY dialog's CANCEL follows its pinned generation: the connection
// lost while the call rings is re-dialed to the old remote with the
// carrier's TLS, the gateway sees the CANCEL, and the switch gets its 487.
func TestCarrierGenerationEarlyCancelRedialsOldRemote(t *testing.T) {
	pki := newTestPKI(t)
	gw1 := startGenGateway(t, pki)
	gw1.sc.ringOnly = true
	r := startGenRig(t, []string{genCarrierHost},
		map[string][]*net.SRV{genCarrierHost: genSRV(gw1.sc.port)}, pki)
	gw2 := startGenGateway(t, pki)

	req, final := r.fs.callAsync(t, sip.Uri{User: "+442071234567", Host: genCarrierHost, Port: 5060},
		r.privateSIP, phoneOfferSDP(r.fs.rtpPort))
	gw1.sc.waitInbound(t, sip.INVITE)

	r.refreshCarrierDNS(t, func() {
		r.stub.srv[genCarrierHost] = genSRV(gw2.sc.port)
	})
	r.waitDest(t, genCarrierHost, gw2.addr)
	gw1.sc.ln.closeAccepted()
	r.waitConnPooledGone(t, gw1.addr)

	if res := r.fsCancel(t, req); res.StatusCode != 200 {
		t.Fatalf("CANCEL answer: got %d, want 200", res.StatusCode)
	}
	gw1.sc.waitInbound(t, sip.CANCEL)
	if n := gw1.sc.ln.accepted(); n != 2 {
		t.Errorf("gateway 1 accepted %d connections, want the CANCEL to redial", n)
	}
	if snis := gw1.sc.snisObserved(); len(snis) != 2 || snis[1] != genCarrierHost {
		t.Errorf("the CANCEL redial SNIs = %v, want a second %q handshake", snis, genCarrierHost)
	}
	select {
	case res := <-final:
		if res == nil || res.StatusCode != 487 {
			code := 0
			if res != nil {
				code = res.StatusCode
			}
			t.Fatalf("the switch's INVITE ended with %d, want 487", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the switch never received the 487")
	}
	waitForRelease(t, r.harness)
	if n := r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()); n != 0 {
		t.Errorf("retiredCount after the cancelled call = %d, want 0", n)
	}
	r.waitConnPooledGone(t, gw1.addr)
	if err := r.probeOldDial(t, gw1.addr); !errors.Is(err, errNoFlow) {
		t.Errorf("a fresh request to the retired address: err = %v, want errNoFlow", err)
	}
}

// A teardown the PROXY originates (the RTP-silence watchdog) on a retired
// generation also re-dials the old remote with the pinned carrier's TLS, and
// both ends get their BYE.
func TestCarrierGenerationProxyTeardownRedialsOldRemote(t *testing.T) {
	pki := newTestPKI(t)
	gw1 := startGenGateway(t, pki)
	r := startGenRig(t, []string{genCarrierHost},
		map[string][]*net.SRV{genCarrierHost: genSRV(gw1.sc.port)}, pki)
	gw2 := startGenGateway(t, pki)
	// No RTP flows in the lab, so the watchdog ends the call on its own.
	r.srv.setRTPTimeout(time.Second)

	res := r.callCarrier(t, gw1, genCarrierHost)
	// Once the call is up nothing here reads it; silence must still tear it
	// down, so mark the switch answer seen and do not ACK again.
	_ = res

	r.refreshCarrierDNS(t, func() {
		r.stub.srv[genCarrierHost] = genSRV(gw2.sc.port)
	})
	r.waitDest(t, genCarrierHost, gw2.addr)
	gw1.sc.ln.closeAccepted()
	r.waitConnPooledGone(t, gw1.addr)

	// The watchdog's BYE to the carrier redials gateway 1 as alpha; the
	// switch gets the matching BYE on its leg.
	gw1.sc.waitInbound(t, sip.BYE)
	if n := gw1.sc.ln.accepted(); n != 2 {
		t.Errorf("gateway 1 accepted %d connections, want the teardown to redial", n)
	}
	if snis := gw1.sc.snisObserved(); len(snis) != 2 || snis[1] != genCarrierHost {
		t.Errorf("gateway 1 SNIs = %v, want a second %q handshake", snis, genCarrierHost)
	}
	if got := r.fs.waitFor(sip.BYE, 1, 5*time.Second); len(got) != 1 {
		t.Errorf("the switch saw %d BYEs from the proxy teardown, want 1", len(got))
	}
	waitForRelease(t, r.harness)
	// The teardown BYEs run after end() under a temporary pin hold; wait
	// for both sends to release it and reclaim the retired connection.
	waitFor(t, 5*time.Second, "the teardown pin to be released", func() bool {
		return r.srv.carrierPins.retiredCount(r.srv.carriers.snapshot()) == 0
	})
	r.waitConnPooledGone(t, gw1.addr)
	if err := r.probeOldDial(t, gw1.addr); !errors.Is(err, errNoFlow) {
		t.Errorf("a fresh request to the retired address: err = %v, want errNoFlow", err)
	}
}

// An address that moves from carrier alpha to carrier beta keeps
// deterministic TLS attribution: the alpha dialog re-dials with alpha's
// SNI, the brand-new beta call dials the same address with beta's.
func TestCarrierGenerationCrossCarrierIdentity(t *testing.T) {
	pki := newTestPKI(t)
	gwA := startGenGateway(t, pki)
	gwC := startGenGateway(t, pki)
	// beta's first-generation port is replaced before it is ever called, so
	// no gateway needs to listen there.
	srv := map[string][]*net.SRV{
		// First generation: alpha at gateway A, beta elsewhere.
		"sip.alpha.test": {{Target: "gw.carrier.test.", Port: uint16(gwA.sc.port), Priority: 10, Weight: 1}},
		"sip.beta.test":  {{Target: "gw.carrier.test.", Port: uint16(1), Priority: 10, Weight: 1}},
	}
	r := startGenRig(t, []string{"sip.alpha.test", "sip.beta.test"}, srv, pki)
	resAlpha := r.callCarrier(t, gwA, "sip.alpha.test")

	// Second generation: alpha moves to C, and beta takes gateway A — the
	// address the alpha dialog was created on.
	r.refreshCarrierDNS(t, func() {
		r.stub.srv["sip.alpha.test"][0].Port = uint16(gwC.sc.port)
		r.stub.srv["sip.beta.test"][0].Port = uint16(gwA.sc.port)
	})
	r.waitDest(t, "sip.alpha.test", gwC.addr)
	r.waitDest(t, "sip.beta.test", gwA.addr)

	gwA.sc.ln.closeAccepted()
	r.waitConnPooledGone(t, gwA.addr)

	// The alpha dialog's BYE re-dials A as alpha...
	if bye := r.fs.uacBye(t, resAlpha); bye.StatusCode != 200 {
		t.Fatalf("alpha BYE after the move: got %d, want 200", bye.StatusCode)
	}
	gwA.sc.waitInbound(t, sip.BYE)
	waitForRelease(t, r.harness)

	// ...and the new beta call dials the SAME address as beta. Neither
	// borrows the other's TLS session.
	resBeta := r.callCarrier(t, gwA, "sip.beta.test")
	if bye := r.fs.uacBye(t, resBeta); bye.StatusCode != 200 {
		t.Fatalf("beta BYE: got %d, want 200", bye.StatusCode)
	}
	gwA.sc.waitInbound(t, sip.BYE)
	// Three handshakes at gateway A: the alpha INVITE, the alpha BYE
	// redial, and the beta INVITE — never one session carrying both names.
	want := []string{"sip.alpha.test", "sip.alpha.test", "sip.beta.test"}
	if got := gwA.sc.snisObserved(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("gateway A SNIs = %v, want %v", got, want)
	}
}
