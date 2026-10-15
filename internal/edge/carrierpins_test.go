package edge

// Tests for the creation-time carrier route registry (carrierdns.go): a
// dialog pins the carrier generation it was born on, so a DNS refresh that
// changes the snapshot moves only NEW transactions while live dialogs keep
// their old remote dialable with their own carrier identity until the last
// one ends and reclaims the retired route.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/freesbc/freesbc/internal/config"
)

// A retained route survives a refresh that retired it; the released one is
// reclaimed by the last dialog, idempotently. Empty and "unknown" carriers
// pin nothing; UDP pins are bookkeeping, never connections.
func TestCarrierRoutePinLifecycle(t *testing.T) {
	stub := &dnsStub{ips: map[string][]string{"gw.example": {"192.0.2.1"}}}
	clk := &fakeClock{t: time.Unix(1000, 0)}
	d := newTestDirectory(stub, clk, nil,
		config.Carrier{Name: "alpha", Host: "gw.example", Port: 5060, ExplicitPort: true, Transport: config.CarrierTCP})
	d.refresh(context.Background())
	old := "192.0.2.1:5060"

	if d.pinCount() != 0 {
		t.Fatalf("pin count = %d, want 0 before any dialog", d.pinCount())
	}
	for _, c := range []string{"", carrierUnknown} {
		rel := d.retain(c, "tcp", old)
		rel()
		if d.pinCount() != 0 {
			t.Errorf("carrier %q created a pin", c)
		}
	}

	rel1 := d.retain("alpha", "tcp", old)
	rel2 := d.retain("alpha", "tcp", old)
	if !d.pinnedRoute("alpha", "tcp", old) {
		t.Fatal("the route is not pinned while dialogs hold it")
	}
	if d.pinCount() != 1 {
		t.Errorf("pin count = %d, want one shared record", d.pinCount())
	}
	rel1()
	if !d.pinnedRoute("alpha", "tcp", old) {
		t.Fatal("the route was reclaimed while a second dialog still holds it")
	}
	rel1() // an idempotent release changes nothing
	if !d.pinnedRoute("alpha", "tcp", old) {
		t.Fatal("a duplicate release reclaimed the route early")
	}
	rel2()
	if d.pinnedRoute("alpha", "tcp", old) || d.pinCount() != 0 {
		t.Fatalf("the last dialog did not reclaim the route (pins=%d)", d.pinCount())
	}
}

// After a successful refresh the snapshot admits only the new generation:
// exactCarrier, carrierFor and the source prefixes no longer name the old
// address, while a dialog born on it keeps its pin. The pin never feeds the
// snapshot, so the retired address cannot be readmitted to new transactions.
func TestCarrierRouteRetirementAcrossRefresh(t *testing.T) {
	stub := &dnsStub{ips: map[string][]string{"gw.example": {"192.0.2.1"}}}
	clk := &fakeClock{t: time.Unix(1000, 0)}
	d := newTestDirectory(stub, clk, nil,
		config.Carrier{Name: "alpha", Host: "gw.example", Port: 5060, ExplicitPort: true, Transport: config.CarrierTCP})
	d.refresh(context.Background())
	old, fresh := "192.0.2.1:5060", "192.0.2.2:5060"
	if name, ok := d.snapshot().exactCarrier("tcp", old); !ok || name != "alpha" {
		t.Fatalf("exactCarrier(old) before refresh = %q,%v", name, ok)
	}

	release := d.retain("alpha", "tcp", old) // the dialog born on the first generation

	stub.mu.Lock()
	stub.ips["gw.example"] = []string{"192.0.2.2"}
	stub.mu.Unlock()
	clk.advance(carrierDNSTTL + time.Second)
	d.refresh(context.Background())

	snap := d.snapshot()
	if got := snap.addrs["alpha"]; len(got) != 1 || got[0] != ap(fresh) {
		t.Fatalf("addrs after refresh = %v, want %s", got, fresh)
	}
	if _, ok := snap.exactCarrier("tcp", old); ok {
		t.Error("a retired address is still a new-transaction target")
	}
	if name, ok := snap.exactCarrier("tcp", fresh); !ok || name != "alpha" {
		t.Errorf("the current address = %q,%v, want alpha,true", name, ok)
	}
	if snap.isSource(ap(old).Addr()) {
		t.Error("the retired address is still a carrier source after the refresh")
	}
	if _, ok := snap.carrierFor(ap(old), "tcp"); ok {
		t.Error("the retired address is still admitted as a carrier's")
	}
	// The existing dialog keeps its route, and is the single owner.
	if !d.pinnedRoute("alpha", "tcp", old) {
		t.Error("the dialog's creation-time route was lost")
	}
	if owner, ok := d.retiredOwner("tcp", old); !ok || owner != "alpha" {
		t.Errorf("retiredOwner = %q,%v, want alpha,true", owner, ok)
	}

	release()
	if _, ok := d.retiredOwner("tcp", old); ok || d.pinCount() != 0 {
		t.Fatalf("the retired route survived its last dialog (pins=%d)", d.pinCount())
	}

	// A failed refresh keeps the last good set: the current generation is
	// unchanged, and with no pin the old question simply has no owner.
	stub.mu.Lock()
	stub.ipErr = errors.New("resolver down")
	stub.mu.Unlock()
	clk.advance(carrierDNSTTL + time.Second)
	d.refresh(context.Background())
	if got := d.snapshot().addrs["alpha"]; len(got) != 1 || got[0] != ap(fresh) {
		t.Errorf("addrs after a failed refresh = %v, want the last good %s", got, fresh)
	}

	// Swing back: the address becoming current again is selected through
	// the snapshot, not through the retired pin.
	stub.mu.Lock()
	stub.ipErr = nil
	stub.ips["gw.example"] = []string{"192.0.2.1"}
	stub.mu.Unlock()
	clk.advance(carrierDNSTTL + time.Second)
	d.refresh(context.Background())
	if _, ok := d.snapshot().exactCarrier("tcp", old); !ok {
		t.Error("an address that became current again is not selected")
	}
}

// The same old destination pinned by two different carriers (each retired
// it on its own generation) has no single owner: a teardown without a hint
// fails closed, while each dialog still reaches its own carrier by name.
func TestCarrierRouteSameAddressTwoCarriers(t *testing.T) {
	stub := &dnsStub{ips: map[string][]string{
		"gw-a.example": {"192.0.2.9"},
		"gw-b.example": {"192.0.2.9"},
	}}
	d := newTestDirectory(stub, &fakeClock{t: time.Unix(1000, 0)}, nil,
		config.Carrier{Name: "alpha", Host: "gw-a.example", Port: 5060, ExplicitPort: true, Transport: config.CarrierTCP},
		config.Carrier{Name: "beta", Host: "gw-b.example", Port: 5060, ExplicitPort: true, Transport: config.CarrierTCP})
	d.refresh(context.Background())
	dest := "192.0.2.9:5060"
	relA := d.retain("alpha", "tcp", dest)
	relB := d.retain("beta", "tcp", dest)
	defer relA()
	defer relB()

	if !d.pinnedRoute("alpha", "tcp", dest) || !d.pinnedRoute("beta", "tcp", dest) {
		t.Fatal("each carrier keeps its own pin on the shared address")
	}
	if owner, ok := d.retiredOwner("tcp", dest); ok {
		t.Errorf("ambiguous ownership resolved to %q; want fail closed", owner)
	}
	// Distinct transports stay distinct routes.
	relU := d.retain("alpha", "udp", dest)
	if owner, ok := d.retiredOwner("udp", dest); !ok || owner != "alpha" {
		t.Errorf("udp owner = %q,%v, want alpha,true", owner, ok)
	}
	relU()
}

// Parallel retain/release against one directory settles at zero with no
// lost reference (run under -race).
func TestCarrierRoutePinConcurrent(t *testing.T) {
	d := newTestDirectory(&dnsStub{}, &fakeClock{t: time.Unix(1000, 0)}, nil)
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			carrier := "alpha"
			if n%2 == 0 {
				carrier = "beta"
			}
			for i := 0; i < 100; i++ {
				rel := d.retain(carrier, "tcp", "192.0.2.5:5060")
				rel()
			}
		}(g)
	}
	wg.Wait()
	if d.pinCount() != 0 {
		t.Errorf("pin count after all releases = %d, want 0", d.pinCount())
	}
}

// A connection serving an early dialog (a reliable 18x a PRACK follows) is
// kept busy by the fork's recorded route, even before the 2xx confirms the
// dialog; ending the dialog releases it.
func TestUsesRemoteCoversEarlyFork(t *testing.T) {
	tab := newDialogTable(NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "3003", Host: "gw.example", Port: 5060})
	from := &sip.FromHeader{Address: sip.Uri{User: "3003", Host: "gw.example"}, Params: sip.NewParams()}
	from.Params.Add("tag", "caller-tag")
	req.AppendHeader(from)
	cid := sip.CallIDHeader("early@example")
	req.AppendHeader(&cid)

	d, res := tab.begin(req, planePrivate, 0)
	if res != beginOK {
		t.Fatalf("begin = %v, want beginOK", res)
	}
	const remote, network = "192.0.2.4:5061", "tls"
	if tab.usesRemote(network, remote) {
		t.Fatal("a fresh dialog is already routed to the carrier remote")
	}
	d.setForkRoute("fork-tag", dialogRoute{publicRemote: remote, transport: network})
	if !tab.usesRemote("TLS", remote) { // transport compares case-insensitively
		t.Error("the early fork's route does not keep the connection busy")
	}
	if tab.usesRemote("tcp", remote) {
		t.Error("the early route matched the wrong transport")
	}
	d.end(endShutdown)
	if tab.usesRemote(network, remote) {
		t.Error("the route still keeps the connection busy after the dialog ended")
	}
}

// A route pin attached right as the dialog ends is released immediately, so
// it never leaks through the shutdown/answer race.
func TestAttachRoutePinAfterEndReleases(t *testing.T) {
	tab := newDialogTable(NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "3003", Host: "gw.example", Port: 5060})
	from := &sip.FromHeader{Address: sip.Uri{User: "3003", Host: "gw.example"}, Params: sip.NewParams()}
	from.Params.Add("tag", "late-tag")
	req.AppendHeader(from)
	cid := sip.CallIDHeader("late@example")
	req.AppendHeader(&cid)
	d, res := tab.begin(req, planePrivate, 0)
	if res != beginOK {
		t.Fatalf("begin = %v, want beginOK", res)
	}
	d.end(endByeCaller)
	released := false
	d.attachRoutePin(func() { released = true })
	if !released {
		t.Error("a pin attached after end() was not released at once")
	}
}
