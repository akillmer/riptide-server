package utp

/*
#include "utp.h"
*/
import "C"
import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"github.com/anacrolix/missinggo/inproc"
	"github.com/anacrolix/mmsg"
)

type Socket struct {
	pc            net.PacketConn
	ctx           *C.utp_context
	backlog       chan *Conn
	closed        bool
	conns         map[*C.utp_socket]*Conn
	nonUtpReads   chan packet
	writeDeadline time.Time
	readDeadline  time.Time
}

var (
	_ net.PacketConn = (*Socket)(nil)
	_ net.Listener   = (*Socket)(nil)
)

type packet struct {
	b    []byte
	from net.Addr
}

func listenPacket(network, addr string) (pc net.PacketConn, err error) {
	if network == "inproc" {
		return inproc.ListenPacket(network, addr)
	}
	return net.ListenPacket(network, addr)
}

func NewSocket(network, addr string) (*Socket, error) {
	pc, err := listenPacket(network, addr)
	if err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	ctx := C.utp_init(2)
	if ctx == nil {
		panic(ctx)
	}
	ctx.setCallbacks()
	if utpLogging {
		ctx.setOption(C.UTP_LOG_NORMAL, 1)
		ctx.setOption(C.UTP_LOG_MTU, 1)
		ctx.setOption(C.UTP_LOG_DEBUG, 1)
	}
	s := &Socket{
		pc:          pc,
		ctx:         ctx,
		backlog:     make(chan *Conn, 5),
		conns:       make(map[*C.utp_socket]*Conn),
		nonUtpReads: make(chan packet, 100),
	}
	libContextToSocket[ctx] = s
	go s.timeoutChecker()
	go s.packetReader()
	return s, nil
}

func (s *Socket) onLibSocketDestroyed(ls *C.utp_socket) {
	delete(s.conns, ls)
}

func (s *Socket) newConn(us *C.utp_socket) *Conn {
	c := &Conn{
		s: us,
	}
	c.cond.L = &mu
	s.conns[us] = c
	c.writeDeadlineTimer = time.AfterFunc(-1, c.cond.Broadcast)
	c.readDeadlineTimer = time.AfterFunc(-1, c.cond.Broadcast)
	return c
}

func (s *Socket) packetReader() {
	mc := mmsg.NewConn(s.pc)
	// Increasing the messages increases the memory use, but also means we can
	// reduces utp_issue_deferred_acks and syscalls which should improve
	// efficiency. On the flip side, not all OSs implement batched reads.
	ms := make([]mmsg.Message, func() int {
		if mc.Err() == nil {
			return 16
		} else {
			return 1
		}
	}())
	for i := range ms {
		// This buffer size is just made up, but it's a trade off between how
		// much of the MTU (usually about 1500?) will be utilized and memory-
		// use.
		ms[i].Buffers = [][]byte{make([]byte, 0x1000)}
	}
	// Some crap OSs like Windoze will raise errors in Reads that don't
	// actually mean we should stop.
	consecutiveErrors := 0
	for {
		// In C, all the reads are processed and when it threatens to block,
		// we're supposed to call utp_issue_deferred_acks.
		n, err := mc.RecvMsgs(ms)
		if n == 1 {
			singleMsgRecvs.Add(1)
		}
		if n > 1 {
			multiMsgRecvs.Add(1)
		}
		if err != nil {
			mu.Lock()
			closed := s.closed
			mu.Unlock()
			if closed {
				// We don't care.
				return
			}
			// See https://github.com/anacrolix/torrent/issues/83. If we get
			// an endless stream of errors (such as the PacketConn being
			// Closed outside of our control, this work around may need to be
			// reconsidered.
			Logger.Printf("ignoring socket read error: %s", err)
			consecutiveErrors++
			if consecutiveErrors >= 100 {
				Logger.Print("too many consecutive errors, closing socket")
				s.Close()
				return
			}
			continue
		}
		consecutiveErrors = 0
		func() {
			mu.Lock()
			defer mu.Unlock()
			if s.closed {
				return
			}
			gotUtp := false
			for _, m := range ms[:n] {
				gotUtp = s.processReceivedMessage(m.Buffers[0][:m.N], m.Addr) || gotUtp
			}
			if gotUtp {
				C.utp_issue_deferred_acks(s.ctx)
				// TODO: When is this done in C?
				C.utp_check_timeouts(s.ctx)
			}
		}()
	}
}

var reads int64

func (s *Socket) processReceivedMessage(b []byte, addr net.Addr) (utp bool) {
	sa, sal := netAddrToLibSockaddr(addr)
	atomic.AddInt64(&reads, 1)
	// log.Printf("received %d bytes, %d packets", n, reads)
	ret := C.utp_process_udp(s.ctx, (*C.byte)(&b[0]), C.size_t(len(b)), sa, sal)
	switch ret {
	case 1:
		socketUtpPacketsReceived.Add(1)
		return true
	case 0:
		s.onReadNonUtp(b, addr)
		return false
	default:
		panic(ret)
	}
}

func (s *Socket) timeoutChecker() {
	for {
		mu.Lock()
		if s.closed {
			mu.Unlock()
			return
		}
		// C.utp_issue_deferred_acks(s.ctx)
		C.utp_check_timeouts(s.ctx)
		mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func (me *Socket) Close() error {
	mu.Lock()
	defer mu.Unlock()
	if me.closed {
		return nil
	}
	// Calling this deletes the pointer. It must not be referred to after
	// this.
	C.utp_destroy(me.ctx)
	me.ctx = nil
	me.pc.Close()
	close(me.backlog)
	close(me.nonUtpReads)
	me.closed = true
	return nil
}

func (me *Socket) Addr() net.Addr {
	return me.pc.LocalAddr()
}

func (me *Socket) LocalAddr() net.Addr {
	return me.pc.LocalAddr()
}

func (s *Socket) Accept() (net.Conn, error) {
	nc, ok := <-s.backlog
	if !ok {
		return nil, errors.New("closed")
	}
	return nc, nil
}

func (s *Socket) Dial(addr string) (net.Conn, error) {
	return s.DialTimeout(addr, 0)
}

func (s *Socket) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return s.DialContext(ctx, addr)
}

func (s *Socket) resolveAddr(addr string) (net.Addr, error) {
	n := s.Addr().Network()
	switch n {
	case "inproc":
		return inproc.ResolveAddr(n, addr)
	default:
		return net.ResolveUDPAddr(n, addr)
	}
}

func (s *Socket) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	ua, err := s.resolveAddr(addr)
	if err != nil {
		panic(err)
	}
	sa, sl := netAddrToLibSockaddr(ua)
	mu.Lock()
	defer mu.Unlock()
	if s.closed {
		return nil, errors.New("socket closed")
	}
	c := s.newConn(C.utp_create_socket(s.ctx))
	C.utp_connect(c.s, sa, sl)
	err = c.waitForConnect(ctx)
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (s *Socket) pushBacklog(c *Conn) {
	select {
	case s.backlog <- c:
	default:
		c.close()
	}
}

func (s *Socket) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	p, ok := <-s.nonUtpReads
	if !ok {
		err = errors.New("closed")
		return
	}
	n = copy(b, p.b)
	addr = p.from
	return
}

func (s *Socket) onReadNonUtp(b []byte, from net.Addr) {
	socketNonUtpPacketsReceived.Add(1)
	select {
	case s.nonUtpReads <- packet{append([]byte(nil), b...), from}:
	default:
		// log.Printf("dropped non utp packet: no room in buffer")
		nonUtpPacketsDropped.Add(1)
	}
}

func (s *Socket) SetReadDeadline(t time.Time) error {
	panic("not implemented")
}

func (s *Socket) SetWriteDeadline(t time.Time) error {
	panic("not implemented")
}

func (s *Socket) SetDeadline(t time.Time) error {
	panic("not implemented")
}

func (s *Socket) WriteTo(b []byte, addr net.Addr) (int, error) {
	return s.pc.WriteTo(b, addr)
}

func (s *Socket) ReadBufferLen() int {
	mu.Lock()
	defer mu.Unlock()
	return int(C.utp_context_get_option(s.ctx, C.UTP_RCVBUF))
}

func (s *Socket) WriteBufferLen() int {
	mu.Lock()
	defer mu.Unlock()
	return int(C.utp_context_get_option(s.ctx, C.UTP_SNDBUF))
}

func (s *Socket) SetWriteBufferLen(len int) {
	mu.Lock()
	defer mu.Unlock()
	i := C.utp_context_set_option(s.ctx, C.UTP_SNDBUF, C.int(len))
	if i != 0 {
		panic(i)
	}
}
