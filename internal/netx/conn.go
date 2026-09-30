package netx

import (
	"bufio"
	"fmt"
	"intranet-play/internal/protocol"
	"net"
	"sync"
	"time"
)

const maxLineSize = 1024 * 1024

type Conn struct {
	raw     net.Conn
	writeMu sync.Mutex
	inbox   chan protocol.Envelope
	done    chan struct{}
}

func NewConn(raw net.Conn) *Conn {
	c := &Conn{
		raw:   raw,
		inbox: make(chan protocol.Envelope, 16),
		done:  make(chan struct{}),
	}
	go c.readLoop()
	return c
}

func (c *Conn) readLoop() {
	defer close(c.inbox)
	defer close(c.done)

	sc := bufio.NewScanner(c.raw)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLineSize)
	for sc.Scan() {
		env, err := protocol.DecodeLine(sc.Bytes())
		if err != nil {
			continue
		}
		c.inbox <- *env
	}
}

func (c *Conn) Send(env protocol.Envelope) error {
	b, err := protocol.Encode(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.raw.Write(b)
	if err != nil {
		return err
	}
	return nil
}

func (c *Conn) Close() error {
	return c.raw.Close()
}

func (c *Conn) Inbox() <-chan protocol.Envelope {
	return c.inbox
}

func (c *Conn) Done() <-chan struct{} {
	return c.done
}
func ListenAndAccept(addr string) (net.Listener, *Conn, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	raw, err := ln.Accept()
	if err != nil {
		ln.Close()
		return nil, nil, err
	}
	return ln, NewConn(raw), nil
}

func Connect(addr string) (*Conn, error) {
	c, err := net.DialTimeout("tcp", addr, time.Second*5)
	if err != nil {
		return nil, err
	}
	return NewConn(c), nil
}
func LocalAddr(port int) string {
	return fmt.Sprintf("0.0.0.0:%d", port)
}
