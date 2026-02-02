package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
)

type Writable interface {
	WriteRaw([]byte) error
	Open() error
	Close() error
}

type UsbWriter struct {
	mu     sync.Mutex
	path   string
	writer io.WriteCloser
}

type TcpWriter struct {
	mu      sync.Mutex
	address string
	conn    net.Conn
}

func (u *UsbWriter) WriteRaw(data []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.writer == nil {
		return errors.New("No active connection. Reconnect")
	}
	_, err := u.writer.Write(data)
	return err
}

func (u *UsbWriter) Open() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	f, err := os.OpenFile(u.path, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return err
	}
	u.writer = f
	return nil
}

func (u *UsbWriter) Close() error {
	fmt.Println("Closing")
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.writer == nil {
		return nil
	}
	err := u.writer.Close()
	u.writer = nil
	return err
}

func (t *TcpWriter) WriteRaw(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		return errors.New("No active connection. Reconnect")
	}
	_, err := t.conn.Write(data)
	return err
}

func (t *TcpWriter) Open() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	conn, err := net.Dial("tcp", t.address)
	if err != nil {
		return err
	}
	t.conn = conn
	return nil
}

func (t *TcpWriter) Close() error {
	fmt.Println("Closing")
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}
