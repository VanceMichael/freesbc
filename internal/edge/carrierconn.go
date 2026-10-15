package edge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/freesbc/freesbc/internal/config"
)

// This file is the outbound half of the carrier stream transports: the tcp
// and tls connections FreeSBC itself opens to a carrier.
//
// sipgo v1.4.3 can dial a stream destination, but it does so with one
// process-wide tls.Config and the destination host it was given, so it
// cannot carry a per-carrier server name, root set or client certificate.
// FreeSBC therefore dials and handshakes the connection itself, wraps it in
// the same streamConn every accepted connection gets (caps, framer, read
// and write deadlines, metrics) and hands it to sipgo through a listener
// that only ever yields such connections (dialListener). sipgo's own
// Serve loop then pools it under its remote address and reads it: a request
// sent to that address finds the connection, responses and the carrier's
// own requests on it reach the normal handlers, and it is closed with
// the transport layer on shutdown. One connection per carrier address is
// reused; when it is lost the next request dials again.
//
// The TLS config sipgo itself would use is made to trust nothing (Run), so a
// connection sipgo dials in a race, after this file's check and before its
// write, can only fail closed.

// dialListener is the net.Listener through which dialed carrier connections
// reach sipgo's tcp or tls transport. It accepts nothing from the network.
type dialListener struct{ addr net.Addr }

func (dialListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (dialListener) Close() error              { return nil }
func (l dialListener) Addr() net.Addr          { return l.addr }

// carrierConns dials and pools the stream connections to carriers.
type carrierConns struct {
	s *Server
	// tls is the client config of each tls carrier, by name, loaded in New
	// so a missing or malformed file fails startup.
	tls map[string]*tls.Config
	// lis hands a dialed connection to sipgo, one per stream transport that
	// has a carrier ("tcp", "tls"). Run serves them beside the public
	// listeners.
	lis map[string]*streamListener

	mu    sync.Mutex
	locks map[string]*sync.Mutex // one dial at a time per destination

	// dialed names the carrier each connection FreeSBC itself opened was
	// established as, keyed by the connection's own local address, which is
	// unique per connection. It is the identity check that keeps a
	// connection dialed for carrier A from being reused for carrier B when
	// a DNS refresh moves one address between them (carrierpin.go). A
	// connection the carrier opened to FreeSBC is not recorded: it is
	// reusable as the carrier address it came from, as before.
	dialed map[string]string
}

// newCarrierConns builds the TLS configs of the tls carriers and the
// listeners for the tcp and tls ones.
func newCarrierConns(s *Server, cfg *config.Config) (*carrierConns, error) {
	cc := &carrierConns{s: s, tls: map[string]*tls.Config{}, lis: map[string]*streamListener{},
		locks: map[string]*sync.Mutex{}, dialed: map[string]string{}}
	for _, c := range cfg.CarrierList() {
		if c.Transport == config.CarrierUDP {
			continue
		}
		if cc.lis[c.Transport] == nil {
			cc.lis[c.Transport] = &streamListener{
				Listener: dialListener{addr: &net.TCPAddr{IP: cfg.PublicBind().AsSlice()}},
				s:        s, transport: c.Transport,
				ready: make(chan net.Conn, 16), done: make(chan struct{}),
			}
		}
		if c.Transport != config.CarrierTLS {
			continue
		}
		conf, err := carrierTLSConfig(c)
		if err != nil {
			return nil, fmt.Errorf("edge.carriers.%s: %w", c.Name, err)
		}
		cc.tls[c.Name] = conf
	}
	return cc, nil
}

// carrierTLSConfig is the client config toward one tls carrier: the server
// certificate is verified against the configured host name (the SIP domain
// of RFC 5922, not an SRV target) and, as SNI, sent to the carrier; ca_file,
// when set, replaces the system roots; a client certificate is presented
// when configured. There is no way to turn verification off.
func carrierTLSConfig(c config.Carrier) (*tls.Config, error) {
	conf := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: c.Host}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca_file %s: no PEM certificate found", c.CAFile)
		}
		conf.RootCAs = pool
	}
	if c.ClientCert != "" {
		cert, err := tls.LoadX509KeyPair(c.ClientCert, c.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("client_cert/client_key: %w", err)
		}
		conf.Certificates = []tls.Certificate{cert}
	}
	return conf, nil
}

// listeners are the dial listeners Run serves.
func (cc *carrierConns) listeners() []listener {
	var out []listener
	for _, tr := range []string{config.CarrierTCP, config.CarrierTLS} {
		if l := cc.lis[tr]; l != nil {
			out = append(out, listener{transport: tr, addr: l.Addr().String(), stream: l})
		}
	}
	return out
}

// destLock is the mutex that serialises dials to one destination.
func (cc *carrierConns) destLock(dest string) *sync.Mutex {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	m := cc.locks[dest]
	if m == nil {
		m = &sync.Mutex{}
		cc.locks[dest] = m
	}
	return m
}

// carrierTarget names the tcp or tls carrier whose resolved address is dest
// in the CURRENT snapshot, for a request leaving over network. It is the
// admission of a brand-new transaction's dial: a dialog's pinned generation
// is answered by carrierPins before this is even consulted (requireFlow).
// It is the only case FreeSBC dials a stream connection: a client's flow is
// never dialed.
func (s *Server) carrierTarget(network, dest string) (string, bool) {
	dest = canonicalPinDest(dest)
	snap := s.carriers.snapshot()
	for _, name := range snap.names {
		if snap.transports[name] != network {
			continue
		}
		for _, a := range snap.addrs[name] {
			if a.String() == dest {
				return name, true
			}
		}
	}
	return "", false
}

// noteDialed records that the connection identified by its own local
// address was opened as carrier. The connection's Close forgets it once.
func (cc *carrierConns) noteDialed(localAddr, carrier string) {
	cc.mu.Lock()
	cc.dialed[localAddr] = carrier
	cc.mu.Unlock()
}

func (cc *carrierConns) forgetDialed(localAddr string) {
	cc.mu.Lock()
	delete(cc.dialed, localAddr)
	cc.mu.Unlock()
}

// dialedCarrier names the carrier a pooled connection FreeSBC itself opened
// was established as. Empty means the connection was accepted from the
// carrier, or is otherwise not one of our dials.
func (cc *carrierConns) dialedCarrier(c sip.Connection) string {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.dialed[c.LocalAddr().String()]
}

// pooledFor reports whether the pooled connection to dest may serve name:
// none pooled (a dial is needed), or the pool holds a connection that is
// reusable for that carrier — one accepted from the carrier, or one dialed
// as that very carrier. A connection dialed for a DIFFERENT carrier (an
// address that moved between carriers) must not be reused: the TLS session
// carries the other carrier's name and certificate.
func (cc *carrierConns) pooledFor(tl *sip.TransportLayer, name, network, dest string) (pooled, reusable bool) {
	c, err := tl.GetConnection(network, dest)
	if err != nil || c == nil {
		return false, false
	}
	owner := cc.dialedCarrier(c)
	_, _ = c.TryClose() // GetConnection took a reference; hand it back
	return true, owner == "" || owner == name
}

// ensure returns once sipgo's pool holds a connection to dest that is
// reusable for name, dialing the carrier when it holds none or only a
// connection another carrier established. It does not hold a reference to
// it.
func (cc *carrierConns) ensure(ctx context.Context, name, network, dest string) error {
	m := cc.destLock(dest)
	m.Lock()
	defer m.Unlock()
	tl := cc.s.srv.TransportLayer()
	if pooled, reusable := cc.pooledFor(tl, name, network, dest); pooled && reusable {
		return nil
	}
	err := cc.dial(ctx, name, network, dest)
	if err != nil {
		cc.s.log.Warn("carrier connection failed",
			"carrier", name, "transport", network, "dest", dest, "err", err)
		return err
	}
	// The connection is in sipgo's pool once its Serve loop has taken it
	// from the listener, a moment after the hand-over. Wait for OURS: a
	// connection another carrier established to the same address may sit in
	// the pool beside it.
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if pooled, reusable := cc.pooledFor(tl, name, network, dest); pooled && reusable {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("carrier connection not pooled")
		}
	}
}

// closeRetired reclaims the connection a reclaimed generation pin served:
// when a carrier route's last dialog ends and the current snapshot no longer
// publishes that carrier at that remote, the pooled connection dialed AS
// that carrier is closed, so a retired address keeps no socket of its own.
// A connection another carrier now owns (the address moved), a connection
// the carrier opened, and a route the snapshot still publishes are left
// alone.
func (cc *carrierConns) closeRetired(name, network, dest string) {
	if current, ok := cc.s.carrierTarget(network, dest); ok && current == name {
		return // still a current route: ordinary pool retention applies
	}
	tl := cc.s.srv.TransportLayer()
	c, err := tl.GetConnection(network, dest)
	if err != nil || c == nil {
		return
	}
	if owner := cc.dialedCarrier(c); owner != "" && owner != name {
		_, _ = c.TryClose() // hand the GetConnection reference back
		return
	}
	cc.s.log.Debug("closing connection of a retired carrier route",
		"carrier", name, "transport", network, "dest", dest)
	_ = c.Close()
}

// dial opens, handshakes and hands over one connection to a carrier.
func (cc *carrierConns) dial(ctx context.Context, name, network, dest string) error {
	s := cc.s
	lim := s.streamLim
	ap, err := netip.ParseAddrPort(dest)
	if err != nil {
		return err
	}
	l := cc.lis[network]
	if l == nil {
		return fmt.Errorf("no %s carrier connections are configured", network)
	}
	dctx, cancel := context.WithTimeout(ctx, lim.handshake)
	defer cancel()
	d := net.Dialer{}
	// Leave from the public address, as every public socket does, so the
	// carrier sees the address it has allowed (a family mismatch dials
	// from the routing table's choice).
	if bind := s.boot.PublicBind(); bind.Is4() == ap.Addr().Is4() {
		d.LocalAddr = &net.TCPAddr{IP: bind.AsSlice()}
	}
	raw, err := d.DialContext(dctx, "tcp", dest)
	if err != nil {
		return err
	}
	if why, ok := s.streams.acquire(ap.Addr(), true, lim); !ok {
		_ = raw.Close()
		return fmt.Errorf("connection cap reached (%s)", streamRefusalLabels[why])
	}
	c := &streamConn{Conn: raw, l: l, ap: ap, remote: dest, lim: lim, fr: &sipFramer{max: lim.maxMessage}, carrier: true}
	// Record who this connection belongs to before it can be pooled, so a
	// request that finds it the instant it appears gets the carrier check
	// right; Close forgets it. Keyed by the connection's own local
	// address, which distinguishes two connections to one remote address.
	local := raw.LocalAddr().String()
	cc.noteDialed(local, name)
	c.onClose = func() { cc.forgetDialed(local) }
	s.metrics.StreamConnOpened(network)
	if network == config.CarrierTLS {
		tc := tls.Client(raw, cc.tls[name])
		c.Conn = tc
		_ = raw.SetDeadline(time.Now().Add(lim.handshake))
		if err := tc.HandshakeContext(dctx); err != nil {
			s.metrics.StreamClosed(closeHandshake)
			_ = c.Close()
			return fmt.Errorf("tls handshake: %w", err)
		}
		_ = raw.SetDeadline(time.Time{})
	}
	select {
	case l.ready <- c:
		return nil
	case <-l.done:
		_ = c.Close()
		return net.ErrClosed
	case <-ctx.Done():
		_ = c.Close()
		return ctx.Err()
	}
}
