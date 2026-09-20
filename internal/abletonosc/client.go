package abletonosc

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hypebeast/go-osc/osc"
)

// defaultIdleRelease is how long the reply port stays bound after the last
// OSC message. The server is registered globally in most MCP clients, so a
// session that touched Live once must not lock every later session out.
const defaultIdleRelease = 60 * time.Second

// ErrReplyPortInUse reports that another process holds the UDP port AbletonOSC
// replies to. AbletonOSC sends every reply to that one fixed port, so only one
// client per machine can receive them at a time.
var ErrReplyPortInUse = errors.New("AbletonOSC reply port is in use")

type waitItem struct {
	ch    chan []interface{}
	timer *time.Timer
}

type Client struct {
	remoteAddr *net.UDPAddr
	localAddr  string
	localPort  int
	timeout    time.Duration

	connMu    sync.Mutex
	conn      net.PacketConn
	idleAfter time.Duration
	idleTimer *time.Timer
	lastUsed  time.Time
	holds     int

	mu      sync.Mutex
	pending map[string][]waitItem
}

// NewClient prepares a client without touching the network. The reply port is
// bound on first use (see ensureConn), so the MCP server can start even while
// another instance holds the port.
func NewClient(remoteHost string, remotePort int, localPort int, timeout time.Duration) (*Client, error) {
	if remoteHost == "" {
		return nil, errors.New("remoteHost is empty")
	}
	if remotePort <= 0 || remotePort > 65535 {
		return nil, fmt.Errorf("invalid remotePort: %d", remotePort)
	}
	if localPort <= 0 || localPort > 65535 {
		return nil, fmt.Errorf("invalid localPort: %d", localPort)
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}

	remoteAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", remoteHost, remotePort))
	if err != nil {
		return nil, fmt.Errorf("resolve remote: %w", err)
	}

	return &Client{
		remoteAddr: remoteAddr,
		// Bind IPv4 loopback explicitly. Listening on 0.0.0.0 can end up IPv6-only
		// on newer Go/macOS and miss AbletonOSC replies to 127.0.0.1.
		localAddr: fmt.Sprintf("127.0.0.1:%d", localPort),
		localPort: localPort,
		timeout:   timeout,
		idleAfter: defaultIdleRelease,
		pending:   make(map[string][]waitItem),
	}, nil
}

// ensureConn binds one UDP socket for both send and receive.
// AbletonOSC always replies to localPort (default 11001); using a single
// socket avoids missed replies when send uses a separate ephemeral port.
func (c *Client) ensureConn() (net.PacketConn, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.lastUsed = time.Now()
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := net.ListenPacket("udp", c.localAddr)
	if err != nil {
		if isAddrInUse(err) {
			return nil, fmt.Errorf("%w: UDP %s is held by another process, most likely an ableton-osc-mcp "+
				"started by a different Claude/Cursor session. Close that session (or find the holder with "+
				"`lsof -nP -iUDP:%d`), then call again; this server re-binds on the next call",
				ErrReplyPortInUse, c.localAddr, c.localPort)
		}
		return nil, fmt.Errorf("listen %s: %w", c.localAddr, err)
	}
	c.conn = conn
	go c.readLoop(conn)
	c.idleTimer = time.AfterFunc(c.idleAfter, c.releaseIfIdle)
	return conn, nil
}

// releaseIfIdle frees the reply port once nothing has used it for idleAfter.
// The next Send or Query binds it again.
func (c *Client) releaseIfIdle() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return
	}
	wait := c.idleAfter - time.Since(c.lastUsed)
	if wait <= 0 && (c.holds > 0 || c.hasPending()) {
		wait = c.idleAfter // a tool is mid-run, or a reply may still be on its way
	}
	if wait > 0 {
		c.idleTimer = time.AfterFunc(wait, c.releaseIfIdle)
		return
	}
	_ = c.conn.Close()
	c.conn = nil
}

func (c *Client) hasPending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending) > 0
}

// isAddrInUse covers both EADDRINUSE (Unix) and WSAEADDRINUSE (Windows, 10048),
// which Go does not fold into one errno.
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && errno == 10048
}

func (c *Client) readLoop(conn net.PacketConn) {
	buf := make([]byte, 65535)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Printf("AbletonOSC read error: %v", err)
			continue
		}
		packet, err := osc.ParsePacket(string(buf[:n]))
		if err != nil {
			log.Printf("AbletonOSC parse error: %v", err)
			continue
		}
		c.dispatchPacket(packet)
	}
}

func (c *Client) dispatchPacket(packet osc.Packet) {
	switch p := packet.(type) {
	case *osc.Message:
		c.handleMessage(p)
	case *osc.Bundle:
		for _, msg := range p.Messages {
			c.handleMessage(msg)
		}
		for _, b := range p.Bundles {
			c.dispatchPacket(b)
		}
	}
}

func (c *Client) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) Send(address string, args ...interface{}) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("address is required")
	}
	msg := osc.NewMessage(address)
	msg.Append(args...)
	data, err := msg.MarshalBinary()
	if err != nil {
		return err
	}
	conn, err := c.ensureConn()
	if err != nil {
		return err
	}
	_, err = conn.WriteTo(data, c.remoteAddr)
	return err
}

func (c *Client) Query(address string, args ...interface{}) ([]interface{}, error) {
	return c.QueryWithTimeout(c.timeout, address, args...)
}

func (c *Client) QueryWithTimeout(timeout time.Duration, address string, args ...interface{}) ([]interface{}, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}
	if timeout <= 0 {
		timeout = c.timeout
	}
	ch := make(chan []interface{}, 1)
	timer := time.NewTimer(timeout)

	c.mu.Lock()
	c.pending[address] = append(c.pending[address], waitItem{ch: ch, timer: timer})
	c.mu.Unlock()

	if err := c.Send(address, args...); err != nil {
		timer.Stop()
		c.mu.Lock()
		c.pending[address] = dropFirstWaiter(c.pending[address], ch)
		if len(c.pending[address]) == 0 {
			delete(c.pending, address)
		}
		c.mu.Unlock()
		return nil, err
	}

	select {
	case res := <-ch:
		return res, nil
	case <-timer.C:
		c.mu.Lock()
		c.pending[address] = dropFirstWaiter(c.pending[address], ch)
		if len(c.pending[address]) == 0 {
			delete(c.pending, address)
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("no response received to query: %s", address)
	}
}

func dropFirstWaiter(queue []waitItem, ch chan []interface{}) []waitItem {
	for i, w := range queue {
		if w.ch == ch {
			queue[i].timer.Stop()
			return append(queue[:i], queue[i+1:]...)
		}
	}
	return queue
}

func (c *Client) handleMessage(msg *osc.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	queue := c.pending[msg.Address]
	if len(queue) == 0 {
		return
	}
	w := queue[0]
	queue = queue[1:]
	if len(queue) == 0 {
		delete(c.pending, msg.Address)
	} else {
		c.pending[msg.Address] = queue
	}
	w.timer.Stop()

	select {
	case w.ch <- msg.Arguments:
	default:
	}
}

// Hold keeps the reply port bound until the returned func is called, however
// quiet the client is in between. The MCP layer wraps every tool call in a
// Hold so a tool that waits out several bars cannot lose the port mid-run.
func (c *Client) Hold() func() {
	c.connMu.Lock()
	c.holds++
	c.connMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.connMu.Lock()
			c.holds--
			c.lastUsed = time.Now() // the idle window starts when the tool ends
			c.connMu.Unlock()
		})
	}
}
