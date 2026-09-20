package abletonosc

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hypebeast/go-osc/osc"
)

// occupyUDPPort binds a loopback UDP port the way another ableton-osc-mcp
// instance would, and returns the port with a func that frees it.
func occupyUDPPort(t *testing.T) (int, func()) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy port: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	return port, func() { _ = conn.Close() }
}

// startFakeAbletonOSC answers every OSC message with the same address and a
// single "ok" argument, sent back to the sender's socket. That matches what
// this client sees from AbletonOSC, because it sends from its reply port.
func startFakeAbletonOSC(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start fake AbletonOSC: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			packet, err := osc.ParsePacket(string(buf[:n]))
			if err != nil {
				continue
			}
			msg, ok := packet.(*osc.Message)
			if !ok {
				continue
			}
			reply := osc.NewMessage(msg.Address)
			reply.Append("ok")
			data, err := reply.MarshalBinary()
			if err != nil {
				continue
			}
			_, _ = conn.WriteTo(data, from)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

// freeUDPPort returns a loopback UDP port number that nothing holds right now.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	port, free := occupyUDPPort(t)
	free()
	return port
}

// canBind reports whether another process could take the port at this moment.
func canBind(port int) bool {
	conn, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitUntilBindable(t *testing.T, port int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !canBind(port) {
		if time.Now().After(deadline) {
			t.Fatalf("reply port %d was still held after %v", port, within)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewClientStartsWhileReplyPortIsBusy(t *testing.T) {
	port, free := occupyUDPPort(t)
	defer free()

	c, err := NewClient("127.0.0.1", 11000, port, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient failed while reply port %d is held elsewhere: %v", port, err)
	}
	defer func() { _ = c.Close() }()
}

func TestQueryExplainsBusyReplyPort(t *testing.T) {
	port, free := occupyUDPPort(t)
	defer free()

	c, err := NewClient("127.0.0.1", 11000, port, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	_, err = c.Query("/live/test")
	if !errors.Is(err, ErrReplyPortInUse) {
		t.Fatalf("Query error = %v, want ErrReplyPortInUse", err)
	}
	// The agent relays this text to a person, so it has to say which port.
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Errorf("error does not name the busy port %d: %v", port, err)
	}
}

func TestQueryRecoversOnceReplyPortIsFree(t *testing.T) {
	serverPort := startFakeAbletonOSC(t)
	port, free := occupyUDPPort(t)

	c, err := NewClient("127.0.0.1", serverPort, port, 500*time.Millisecond)
	if err != nil {
		free()
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	if _, err := c.Query("/live/test"); !errors.Is(err, ErrReplyPortInUse) {
		free()
		t.Fatalf("Query while busy = %v, want ErrReplyPortInUse", err)
	}

	free() // the other session ends

	res, err := c.Query("/live/test")
	if err != nil {
		t.Fatalf("Query after the port was freed: %v", err)
	}
	if len(res) != 1 || res[0] != "ok" {
		t.Errorf("Query result = %v, want [ok]", res)
	}
}

func TestIdleClientReleasesReplyPort(t *testing.T) {
	serverPort := startFakeAbletonOSC(t)
	port := freeUDPPort(t)

	c, err := NewClient("127.0.0.1", serverPort, port, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()
	c.idleAfter = 40 * time.Millisecond

	if _, err := c.Query("/live/test"); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if canBind(port) {
		t.Fatal("reply port was not held right after a query")
	}

	// A session that went quiet must not keep other sessions locked out.
	waitUntilBindable(t, port, time.Second)
}

func TestHeldClientKeepsReplyPortWhileQuiet(t *testing.T) {
	serverPort := startFakeAbletonOSC(t)
	port := freeUDPPort(t)

	c, err := NewClient("127.0.0.1", serverPort, port, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer func() { _ = c.Close() }()
	c.idleAfter = 40 * time.Millisecond

	release := c.Hold()
	if _, err := c.Query("/live/test"); err != nil {
		t.Fatalf("Query: %v", err)
	}

	// A tool waiting out a few bars sends nothing for a while. Losing the port
	// here could leave Live recording with no way to send the stop.
	time.Sleep(200 * time.Millisecond)
	if canBind(port) {
		t.Fatal("reply port was released while a tool call was still running")
	}

	release()
	waitUntilBindable(t, port, time.Second)
}
