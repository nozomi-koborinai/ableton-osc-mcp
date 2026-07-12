package abletonosc

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/hypebeast/go-osc/osc"
)

type waitItem struct {
	ch    chan []interface{}
	timer *time.Timer
}

type Client struct {
	remoteAddr *net.UDPAddr
	timeout    time.Duration

	conn net.PacketConn

	mu      sync.Mutex
	pending map[string][]waitItem
}

// NewClient binds one UDP socket for both send and receive.
// AbletonOSC always replies to localPort (default 11001); using a single
// socket avoids missed replies when send uses a separate ephemeral port.
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

	// Bind IPv4 loopback explicitly. Listening on 0.0.0.0 can end up IPv6-only
	// on newer Go/macOS and miss AbletonOSC replies to 127.0.0.1.
	localAddr := fmt.Sprintf("127.0.0.1:%d", localPort)
	conn, err := net.ListenPacket("udp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", localAddr, err)
	}

	c := &Client{
		remoteAddr: remoteAddr,
		timeout:    timeout,
		conn:       conn,
		pending:    make(map[string][]waitItem),
	}

	go c.readLoop()
	return c, nil
}

func (c *Client) readLoop() {
	buf := make([]byte, 65535)
	for {
		n, _, err := c.conn.ReadFrom(buf)
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
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
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
	_, err = c.conn.WriteTo(data, c.remoteAddr)
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
