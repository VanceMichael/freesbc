package edge

import (
	"net/netip"
	"sync"
)

// This file is the carrier address GENERATION table: the co-ownership, by
// the carrier directory's snapshot generation and the dialogs created from
// it, of a carrier route captured when a call begins.
//
// A successful DNS refresh swaps the directory snapshot atomically: a new
// out-of-dialog transaction is selected from the new snapshot only
// (carrierDest, carrierTarget), and an address the snapshot retired can
// never be admitted into a new transaction again — not by selection and not
// by borrowing a dialog that still runs. A dialog, however, was answered on
// the generation it was created with: its ACK, BYE, PRACK, UPDATE, INFO and
// NOTIFY, their retransmissions, a CANCEL while it is early and the BYEs the
// proxy itself originates must keep the creation-time carrier identity,
// transport and remote address for the dialog's whole life. The pooled
// stream connection is reused while it lives; once it is lost, the pinned
// carrier name is what the redial is made with — that carrier's TLS server
// name, root set and client certificate — even when the same address has
// since moved to another carrier, or out of the snapshot altogether.
//
// UDP has no pin: there is no connection to reuse or re-establish, so a
// datagram keeps going to the dialog's remote with no state here. A
// carrier_sources-only match (carrier "unknown") is not pinned either: it is
// not a configured carrier FreeSBC could dial or name a TLS config for.
//
// One pin is acquired per carrier dialog, right after the record opens and
// before anything is sent, and released exactly once in the dialog's single
// end() transition — so CANCEL, a non-2xx, a timeout, a media failure, BYE,
// shutdown and concurrent endings all have one deterministic owner and an
// idempotent release. When the last dialog holding a retired route ends,
// its state is reclaimed and the old address dials no more.

// carrierPin is one carrier route a dialog captured at creation: the
// edge.carriers name, the stream network ("tcp"/"tls") and the canonical
// remote "IP:port".
type carrierPin struct {
	carrier string
	network string
	dest    string
}

// carrierPins is the reference-counted set of carrier routes live dialogs
// captured. A triple's reference count is the number of live dialogs on it.
// The same (carrier, network, dest) that the current snapshot still
// publishes is held here too: the pin — never the snapshot — answers for a
// dialog for its whole life, which is what keeps its attribution stable
// across a refresh that moves the address to another carrier and back.
type carrierPins struct {
	mu   sync.Mutex
	refs map[carrierPin]int
}

func newCarrierPins() *carrierPins {
	return &carrierPins{refs: map[carrierPin]int{}}
}

// retain records one owner of pin. Callers hold the dialog table lock (the
// table lock is always taken before this one), so retain and release are
// atomic with the dialog's own state transition.
func (p *carrierPins) retain(pin carrierPin) {
	p.mu.Lock()
	p.refs[pin]++
	p.mu.Unlock()
}

// release drops one owner of pin and reports whether it reclaimed the
// route (the last owner went). It balances exactly one retain. The table
// that owns the registry fires its onPinRetire hook only on a true report,
// outside the table lock.
func (p *carrierPins) release(pin carrierPin) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, existed := p.refs[pin]
	if !existed {
		return false
	}
	if n > 1 {
		p.refs[pin] = n - 1
		return false
	}
	// The last owner goes: the retired route is reclaimed.
	delete(p.refs, pin)
	return true
}

// allows reports whether a live dialog pins exactly carrier at dest over
// network. It is the dial authorization for in-dialog traffic: the carrier
// named must be the one that owned the address when the dialog was created.
func (p *carrierPins) allows(carrier, network, dest string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.refs[carrierPin{carrier: carrier, network: network, dest: canonicalPinDest(dest)}] > 0
}

// pinsRemote reports whether any live dialog's carrier route ends at dest
// over network, whatever the carrier: a connection to it carries a call and
// must not be reaped for being quiet while the dialog is still early (a
// confirmed dialog is protected through dialogTable.usesRemote as well).
func (p *carrierPins) pinsRemote(network, dest string) bool {
	dest = canonicalPinDest(dest)
	p.mu.Lock()
	defer p.mu.Unlock()
	for pin := range p.refs {
		if pin.network == network && pin.dest == dest {
			return true
		}
	}
	return false
}

// retiredCount is how many pinned routes the given snapshot no longer
// publishes for their carrier: the state waiting on its last dialog before
// reclamation. Tests and diagnostics use it; new transactions never read it.
func (p *carrierPins) retiredCount(snap *carrierSnapshot) int {
	current := map[carrierPin]bool{}
	if snap != nil {
		for name, addrs := range snap.addrs {
			network := snap.transports[name]
			if streamIndex(network) < 0 {
				continue
			}
			for _, a := range addrs {
				current[carrierPin{carrier: name, network: network, dest: a.String()}] = true
			}
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for pin := range p.refs {
		if !current[pin] {
			n++
		}
	}
	return n
}

// canonicalPinDest renders a destination as the registry keys it: the
// canonical "IP:port" netip form (an IPv6 address bracketed, a mapped v4
// address unmapped), leaving a value that is not an IP:port untouched so a
// malformed destination simply never matches.
func canonicalPinDest(dest string) string {
	if a, err := netip.ParseAddrPort(dest); err == nil {
		// Unmap, as the rest of the carrier code does: an IPv4-mapped IPv6
		// socket address and the v4 form are one destination.
		a = netip.AddrPortFrom(a.Addr().Unmap().WithZone(""), a.Port())
		return a.String()
	}
	return dest
}

// holdPinForTeardown takes a temporary extra reference to the dialog's
// carrier pin for a teardown that runs AFTER end() detached the record — the
// BYEs FreeSBC originates itself when it ends the call (a media timeout, an
// answer it could not anchor). end() then releases the dialog's own
// reference while this hold keeps the route authorized; the teardown
// releases it when the BYEs are sent. nil for a dialog with no carrier pin.
// It must be taken before end().
func (d *dialog) holdPinForTeardown() *carrierPin {
	t := d.tab
	t.mu.Lock()
	defer t.mu.Unlock()
	if d.pin == nil {
		return nil
	}
	pin := *d.pin
	t.pins.retain(pin)
	return &pin
}

// pinCarrier captures this dialog's creation-time carrier route: the
// carrier, the stream transport and the remote the carrier leg was opened
// with. It is taken once, before the dialog's first request is sent, and
// lives until end(). A non-stream transport, no carrier and the "unknown"
// carrier_source match take no pin. A dialog that has already ended (a
// shutdown racing setup) takes none either: its end() has nothing to
// release.
func (d *dialog) pinCarrier(carrier, network, dest string) {
	if streamIndex(network) < 0 || carrier == "" || carrier == carrierUnknown || dest == "" {
		return
	}
	pin := carrierPin{carrier: carrier, network: network, dest: canonicalPinDest(dest)}
	t := d.tab
	t.mu.Lock()
	defer t.mu.Unlock()
	if d.state == dialogEnded || d.pin != nil {
		return
	}
	d.pin = &pin
	t.pins.retain(pin)
}

// carrierRouteHint is the routing hint a send carries toward a carrier: the
// carrier the connection must be made as when no pooled one exists. The zero
// value means "no pinned carrier; resolve the current snapshot". It is set
// for traffic on a recorded carrier dialog toward its public side and for
// fresh carrier transactions; a hint alone authorizes nothing — requireFlow
// re-checks the pin or the snapshot.
type carrierRouteHint struct {
	name string
}

func carrierHint(name string) carrierRouteHint {
	if name == "" || name == carrierUnknown {
		return carrierRouteHint{}
	}
	return carrierRouteHint{name: name}
}

// dialogCarrierHint is the carrier routing hint for traffic headed to side
// to on d's behalf: set toward a carrier dialog's public side only.
func (s *Server) dialogCarrierHint(d *dialog, to side) carrierRouteHint {
	if d == nil || to.plane != planePublic {
		return carrierRouteHint{}
	}
	return carrierHint(d.carrierName())
}
