package transport

import (
    "testing"
    "sync"
)

func TestTCPConnectAndListen(t *testing.T) {
    tcp := NewTCPTransport()

    listener, err := tcp.Listen("127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }
    defer listener.Close()

    addr := listener.Addr().String()

    var wg sync.WaitGroup
    wg.Add(1)

    go func() {
        defer wg.Done()
        conn, err := listener.Accept()
        if err != nil {
            t.Error(err)
            return
        }
        defer conn.Close()

        data, err := conn.Read()
        if err != nil {
            t.Error(err)
            return
        }

        if string(data) != "hello" {
            t.Errorf("expected 'hello', got '%s'", string(data))
        }
    }()

    client, err := tcp.Connect(addr)
    if err != nil {
        t.Fatal(err)
    }
    defer client.Close()

    err = client.Write([]byte("hello"))
    if err != nil {
        t.Fatal(err)
    }

    wg.Wait()
}
