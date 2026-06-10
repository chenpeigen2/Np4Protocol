package transport

import "net"

type Conn interface {
    Read() ([]byte, error)
    Write(data []byte) error
    Close() error
    RemoteAddr() net.Addr
}

type Listener interface {
    Accept() (Conn, error)
    Close() error
    Addr() net.Addr
}

type Transport interface {
    Connect(addr string) (Conn, error)
    Listen(addr string) (Listener, error)
}
