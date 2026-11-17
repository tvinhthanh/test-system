package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Forwarder is a sandbox's only way out. The container's /etc/hosts sends
// the environment's host name to the forwarder, the network has no gateway
// and no DNS that answers, so every connection the script makes lands
// here, whatever tool made it and whatever proxy settings it ignores. The
// forwarder resolved the host once, judged each address with Blocked, and
// pipes the bytes to the address it judged — TLS stays end to end, and a
// Host header or SNI naming another site still reaches only this one.
type Forwarder struct {
	ln      net.Listener
	targets []netip.AddrPort
	wg      sync.WaitGroup
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	slots   chan struct{}
	closed  bool
	Dialer  func(ctx context.Context, network, addr string) (net.Conn, error)
	Timeout time.Duration
}

// Resolver looks a name up (net.DefaultResolver.LookupNetIP).
type Resolver func(ctx context.Context, network, host string) ([]netip.Addr, error)

// MaxConns open at once through one forwarder.
const MaxConns = 512

// Targets resolves host and keeps the addresses the guard allows, with port.
func Targets(ctx context.Context, resolve Resolver, host string, port uint16, allowPrivate bool, allow ...netip.Addr) ([]netip.AddrPort, error) {
	if resolve == nil {
		resolve = net.DefaultResolver.LookupNetIP
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		addrs, err = resolve(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("%s does not resolve (%v)", host, err)
		}
	}
	out := []netip.AddrPort{}
	why := ""
	for _, a := range addrs {
		if r := Blocked(a, allowPrivate, allow...); r != "" {
			why = r
			continue
		}
		out = append(out, netip.AddrPortFrom(a.Unmap(), port))
	}
	if len(out) == 0 {
		if why == "" {
			why = "no address"
		}
		return nil, fmt.Errorf("%s is not reachable from a sandbox: %s", host, why)
	}
	return out, nil
}

// Forward listens on listen and sends every connection to targets.
func Forward(listen string, targets []netip.AddrPort) (*Forwarder, error) {
	if len(targets) == 0 {
		return nil, errors.New("sandbox: nothing to forward to")
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	f := &Forwarder{ln: ln, targets: targets, conns: map[net.Conn]struct{}{}, slots: make(chan struct{}, MaxConns), Timeout: 10 * time.Second}
	f.wg.Add(1)
	go f.serve()
	return f, nil
}

// Addr is where it listens.
func (f *Forwarder) Addr() net.Addr { return f.ln.Addr() }

func (f *Forwarder) serve() {
	defer f.wg.Done()
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		select {
		case f.slots <- struct{}{}:
		default:
			c.Close() // too many at once
			continue
		}
		if !f.track(c, true) {
			c.Close()
			<-f.slots
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer func() { <-f.slots }()
			defer f.track(c, false)
			f.pipe(c)
		}()
	}
}

func (f *Forwarder) track(c net.Conn, add bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if add {
		if f.closed {
			return false
		}
		f.conns[c] = struct{}{}
		return true
	}
	delete(f.conns, c)
	return true
}

func (f *Forwarder) pipe(c net.Conn) {
	defer c.Close()
	dial := f.Dialer
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	var up net.Conn
	for _, t := range f.targets {
		ctx, cancel := context.WithTimeout(context.Background(), f.Timeout)
		conn, err := dial(ctx, "tcp", net.JoinHostPort(t.Addr().String(), strconv.Itoa(int(t.Port()))))
		cancel()
		if err == nil {
			up = conn
			break
		}
	}
	if up == nil {
		return
	}
	if !f.track(up, true) {
		up.Close()
		return
	}
	defer f.track(up, false)
	defer up.Close()
	done := make(chan struct{}, 2)
	half := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
		done <- struct{}{}
	}
	go half(up, c)
	go half(c, up)
	<-done
	<-done
}

// Close stops listening and cuts every open connection.
func (f *Forwarder) Close() error {
	f.mu.Lock()
	f.closed = true
	for c := range f.conns {
		c.Close()
	}
	f.mu.Unlock()
	err := f.ln.Close()
	f.wg.Wait()
	return err
}
