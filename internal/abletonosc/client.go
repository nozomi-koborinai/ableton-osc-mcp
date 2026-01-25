package abletonosc

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hypebeast/go-osc/osc"
)

type anyDispatcher struct {
	onMessage func(msg *osc.Message)
}

func (d anyDispatcher) Dispatch(packet osc.Packet) {
	switch p := packet.(type) {
	case *osc.Message:
		d.onMessage(p)
	case *osc.Bundle:
		for _, msg := range p.Messages {
			d.onMessage(msg)
		}
		for _, b := range p.Bundles {
			d.Dispatch(b)
		}
	default:
		// ignore
	}
}

type waitItem struct {
	ch    chan []interface{}
	timer *time.Timer
}

type Client struct {
	remoteHost string
	remotePort int
	timeout    time.Duration

	client *osc.Client
	server *osc.Server

	mu      sync.Mutex
	pending map[string][]waitItem
}

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

	c := &Client{
		remoteHost: remoteHost,
		remotePort: remotePort,
		timeout:    timeout,
		client:     osc.NewClient(remoteHost, remotePort),
		pending:    make(map[string][]waitItem),
	}

	addr := fmt.Sprintf("0.0.0.0:%d", localPort)
	c.server = &osc.Server{
		Addr: addr,
		Dispatcher: anyDispatcher{
			onMessage: c.handleMessage,
		},
	}

	go func() {
		// ListenAndServe will return on CloseConnection(); that's fine.
		if err := c.server.ListenAndServe(); err != nil {
			// Avoid stdout: MCP uses stdout. log writes to stderr.
			log.Printf("AbletonOSC listen ended: %v", err)
		}
	}()

	return c, nil
}

func (c *Client) Close() error {
	if c.server != nil {
		return c.server.CloseConnection()
	}
	return nil
}

func (c *Client) Send(address string, args ...interface{}) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("address is required")
	}
	msg := osc.NewMessage(address)
	msg.Append(args...)
	return c.client.Send(msg)
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
	// dropFirstWaiter removes the first matching waiter in FIFO order.
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
