package edge

import (
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"testing"

	"github.com/emiago/sipgo/sip"
)

// This file tests the carrier generation table (carrierpin.go): what a
// dialog pins at creation, what a later send may dial, and when the retired
// state is reclaimed. The socket-level behaviour is in
// carrier_generation_test.go.

func newPinDialog(t *testing.T, tab *dialogTable, callID string) *dialog {
	t.Helper()
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "2002", Host: "example.com"})
	from := &sip.FromHeader{Address: sip.Uri{User: "1001", Host: "example.com"}, Params: sip.NewParams()}
	from.Params.Add("tag", "caller-"+callID)
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: "2002", Host: "example.com"}, Params: sip.NewParams()})
	cid := sip.CallIDHeader(callID)
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.INVITE})
	d, res := tab.begin(req, planePrivate, 0)
	if res != beginOK {
		t.Fatalf("begin = %v", res)
	}
	return d
}

func newPinTable() *dialogTable {
	return newDialogTable(NewMetrics(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// A pin authorizes exactly the (carrier, transport, remote) triple the
// dialog captured; another carrier, transport or address gets nothing. The
// retired state counts only routes the snapshot no longer publishes, and it
// is reclaimed with the last dialog.
func TestCarrierPinsAuthorizationAndReclamation(t *testing.T) {
	tab := newPinTable()
	pins := tab.pins
	snap := &carrierSnapshot{
		names:      []string{"alpha"},
		addrs:      map[string][]netip.AddrPort{"alpha": {ap("192.0.2.2:5061")}},
		transports: map[string]string{"alpha": "tls"},
	}

	d1 := newPinDialog(t, tab, "call-1")
	d1.pinCarrier("alpha", "tls", "192.0.2.1:5061")
	if d1.pin == nil {
		t.Fatal("the dialog took no tls carrier pin")
	}
	if !pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("allows refused the pinned carrier triple")
	}
	for _, c := range []struct{ carrier, network, dest string }{
		{"beta", "tls", "192.0.2.1:5061"},  // another carrier at the old address
		{"alpha", "tcp", "192.0.2.1:5061"}, // another transport
		{"alpha", "tls", "192.0.2.9:5061"}, // another remote
	} {
		if pins.allows(c.carrier, c.network, c.dest) {
			t.Errorf("allows accepted a foreign triple %+v", c)
		}
	}
	if !pins.pinsRemote("tls", "192.0.2.1:5061") {
		t.Error("pinsRemote lost the pinned remote")
	}
	if n := pins.retiredCount(snap); n != 1 {
		t.Errorf("retiredCount = %d, want 1 (192.0.2.1 left the snapshot)", n)
	}

	// A second call on the same generation shares the entry; one ending
	// leaves the route alive for the other.
	d2 := newPinDialog(t, tab, "call-2")
	d2.pinCarrier("alpha", "tls", "192.0.2.1:5061")
	if d1.end(endByeCaller) {
		t.Error("an early dialog cannot end as confirmed")
	}
	if !pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("the first dialog ending retired the route another dialog still uses")
	}
	d2.end(endShutdown)
	if pins.allows("alpha", "tls", "192.0.2.1:5061") || pins.pinsRemote("tls", "192.0.2.1:5061") {
		t.Error("the retired route survived its last dialog")
	}
	if n := pins.retiredCount(snap); n != 0 {
		t.Errorf("retiredCount after reclamation = %d, want 0", n)
	}
}

// Pinning is a no-op for udp (there is no pseudo-connection), for the
// "unknown" carrier_sources match, with no dest, and after the dialog ended;
// repeating it never double-counts.
func TestCarrierPinsSkipsUDPUnknownAndEnded(t *testing.T) {
	tab := newPinTable()
	d := newPinDialog(t, tab, "call-skip")
	for _, c := range []struct{ carrier, network, dest string }{
		{"alpha", "udp", "192.0.2.1:5060"},
		{carrierUnknown, "tls", "192.0.2.1:5061"},
		{"", "tls", "192.0.2.1:5061"},
		{"alpha", "tls", ""},
	} {
		d.pinCarrier(c.carrier, c.network, c.dest)
		if d.pin != nil {
			t.Fatalf("pinCarrier(%+v) recorded a pin", c)
		}
	}
	if n := len(tab.pins.refs); n != 0 {
		t.Errorf("the table holds %d refs, want none", n)
	}

	d.pinCarrier("alpha", "tls", "192.0.2.1:5061")
	first := *d.pin
	d.pinCarrier("alpha", "tls", "192.0.2.1:5061") // idempotent
	d.pinCarrier("beta", "tls", "192.0.2.1:5061")  // the first pin sticks
	if *d.pin != first {
		t.Error("the dialog's pin changed or double-acquired")
	}
	if n := tab.pins.refs[first]; n != 1 {
		t.Errorf("refs = %d, want 1", n)
	}
	d.end(endShutdown)
	// Too late: a shutdown racing setup must not leave a pin behind.
	d.pinCarrier("alpha", "tls", "192.0.2.1:5061")
	if n := len(tab.pins.refs); n != 0 {
		t.Errorf("a pin acquired after end left %d refs", n)
	}
}

// end() releases the pin exactly once however many endings race it: CANCEL,
// a non-2xx, a timeout, a media fault, BYE and shutdown all converge here.
func TestCarrierPinsConcurrentEndReleasesOnce(t *testing.T) {
	tab := newPinTable()
	pins := tab.pins
	const n = 16
	var dialogs []*dialog
	for i := 0; i < n; i++ {
		d := newPinDialog(t, tab, "race-"+string(rune('a'+i)))
		d.pinCarrier("alpha", "tls", "192.0.2.1:5061")
		dialogs = append(dialogs, d)
	}
	// All 16 share one triple, so the entry must read exactly 16...
	if got := pins.refs[carrierPin{carrier: "alpha", network: "tls", dest: "192.0.2.1:5061"}]; got != n {
		t.Fatalf("refs = %d, want %d", got, n)
	}
	var wg sync.WaitGroup
	for _, d := range dialogs {
		wg.Add(1)
		go func(d *dialog) {
			defer wg.Done()
			for j := 0; j < 4; j++ { // redundant endings are no-ops
				d.end(endShutdown)
			}
		}(d)
	}
	wg.Wait()
	if len(pins.refs) != 0 {
		t.Errorf("pins left after racing ends: %v", pins.refs)
	}
	if tab.sessionCount() != 0 {
		t.Errorf("session count after the race = %d, want 0", tab.sessionCount())
	}
}

// A proxy teardown (the BYEs the proxy itself sends after end()) holds the
// pin across the dialog's exit: the route stays authorized while the BYEs
// are sent, and the hold reclaims it afterwards.
func TestCarrierPinsTeardownHoldSurvivesEnd(t *testing.T) {
	tab := newPinTable()
	pins := tab.pins
	d := newPinDialog(t, tab, "teardown")
	d.pinCarrier("alpha", "tls", "192.0.2.1:5061")

	hold := d.holdPinForTeardown()
	if hold == nil {
		t.Fatal("the teardown hold is nil for a pinned dialog")
	}
	// The dialog ends (e.g. the media watchdog fired): its own reference is
	// gone but the teardown BYEs still route.
	d.end(endRTPSilence)
	if !pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("the pinned route refused a teardown send after end()")
	}
	if n := pins.retiredCount(&carrierSnapshot{}); n != 1 {
		t.Errorf("retiredCount while the teardown runs = %d, want 1", n)
	}
	// The teardown sends complete: the hold is the last owner.
	if !pins.release(*hold) {
		t.Error("releasing the teardown hold did not reclaim the route")
	}
	if pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("the route survived its teardown hold")
	}

	// No pin on a non-carrier dialog: no hold.
	d2 := newPinDialog(t, tab, "teardown-none")
	if h := d2.holdPinForTeardown(); h != nil {
		t.Error("a dialog without a carrier pin yielded a teardown hold")
	}
	d2.end(endShutdown)
}

// Destination keys are canonical: an IPv4-mapped IPv6 address and the same
// AddrPort are one pin, so a redial's lookup cannot miss on notation.
func TestCarrierPinsCanonicalDest(t *testing.T) {
	pins := newCarrierPins()
	pins.retain(carrierPin{carrier: "alpha", network: "tls", dest: canonicalPinDest("[::ffff:192.0.2.1]:5061")})
	if !pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("the mapped-IPv6 pin does not authorize the canonical IPv4 dest")
	}
	if !pins.allows("alpha", "tls", "192.0.2.1:5061") {
		t.Error("the pin does not authorize its own dest")
	}
	if n := len(pins.refs); n != 1 {
		t.Errorf("the two notations created %d entries, want 1", n)
	}
}

// The routing hint is empty unless it names a configured carrier AND the
// send heads to a carrier dialog's public side.
func TestCarrierRouteHint(t *testing.T) {
	if carrierHint("").name != "" || carrierHint(carrierUnknown).name != "" {
		t.Error("an empty or unknown carrier yielded a routing hint")
	}
	if h := carrierHint("alpha"); h.name != "alpha" {
		t.Errorf("carrierHint(alpha) = %q", h.name)
	}
	tab := newPinTable()
	d := newPinDialog(t, tab, "hint")
	d.setCarrier("alpha")
	var s Server
	priv := side{plane: planePrivate}
	pub := side{plane: planePublic}
	if h := s.dialogCarrierHint(d, priv); h.name != "" {
		t.Error("a send toward the private side carried a carrier hint")
	}
	if h := s.dialogCarrierHint(nil, pub); h.name != "" {
		t.Error("a send without a dialog carried a carrier hint")
	}
	if h := s.dialogCarrierHint(d, pub); h.name != "alpha" {
		t.Errorf("the public carrier hint = %q, want alpha", h.name)
	}
	d.end(endShutdown)
}
