package transport

import (
    "encoding/binary"
    "io"
    "net"
)

type TCPConn struct {
    conn net.Conn
}

func (c *TCPConn) Read() ([]byte, error) {
    var length uint32
    err := binary.Read(c.conn, binary.BigEndian, &length)
    if err != nil {
        return nil, err
    }

    data := make([]byte, length)
    _, err = io.ReadFull(c.conn, data)
    return data, err
}

func (c *TCPConn) Write(data []byte) error {
    err := binary.Write(c.conn, binary.BigEndian, uint32(len(data)))
    if err != nil {
        return err
    }
    _, err = c.conn.Write(data)
    return err
}

func (c *TCPConn) Close() error {
    return c.conn.Close()
}

func (c *TCPConn) RemoteAddr() net.Addr {
    return c.conn.RemoteAddr()
}

type TCPListener struct {
    listener net.Listener
}

func (l *TCPListener) Accept() (Conn, error) {
    conn, err := l.listener.Accept()
    if err != nil {
        return nil, err
    }
    return &TCPConn{conn: conn}, nil
}

func (l *TCPListener) Close() error {
    return l.listener.Close()
}

func (l *TCPListener) Addr() net.Addr {
    return l.listener.Addr()
}

type TCPTransport struct{}

func NewTCPTransport() *TCPTransport {
    return &TCPTransport{}
}

func (t *TCPTransport) Connect(addr string) (Conn, error) {
    conn, err := net.Dial("tcp", addr)
    if err != nil {
        return nil, err
    }
    return &TCPConn{conn: conn}, nil
}

func (t *TCPTransport) Listen(addr string) (Listener, error) {
    listener, err := net.Listen("tcp", addr)
    if err != nil {
        return nil, err
    }
    return &TCPListener{listener: listener}, nil
}
