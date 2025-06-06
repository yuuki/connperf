package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestServeTCP(t *testing.T) {
	originalAddrs := listenAddrs
	defer func() { listenAddrs = originalAddrs }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port := findAvailablePort()
	listenAddrs = []string{fmt.Sprintf("127.0.0.1:%d", port)}

	var wg sync.WaitGroup
	wg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer wg.Done()
		serveTCP(serverCtx)
	}()

	time.Sleep(300 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		serverCancel()
		select {
		case <-time.After(time.Second):
			t.Log("Server shutdown timeout")
		case <-func() <-chan struct{} {
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			return done
		}():
		}
		t.Fatalf("Failed to connect to TCP server: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Second))

	testData := "Hello, TCP!"
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write to TCP connection: %v", err)
	}

	buffer := make([]byte, len(testData))
	if _, err := conn.Read(buffer); err != nil {
		t.Fatalf("Failed to read from TCP connection: %v", err)
	}

	if string(buffer) != testData {
		t.Errorf("Expected %q, got %q", testData, string(buffer))
	}

	serverCancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("Server shutdown timeout")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		return done
	}():
	}
}

func TestServeUDP(t *testing.T) {
	originalAddrs := listenAddrs
	defer func() { listenAddrs = originalAddrs }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port := findAvailablePort()
	listenAddrs = []string{fmt.Sprintf("127.0.0.1:%d", port)}

	var wg sync.WaitGroup
	wg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer wg.Done()
		serveUDP(serverCtx)
	}()

	time.Sleep(300 * time.Millisecond)

	conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		serverCancel()
		select {
		case <-time.After(time.Second):
			t.Log("Server shutdown timeout")
		case <-func() <-chan struct{} {
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			return done
		}():
		}
		t.Fatalf("Failed to connect to UDP server: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(time.Second))

	testData := "Hello, UDP!"
	if _, err := conn.Write([]byte(testData)); err != nil {
		t.Fatalf("Failed to write to UDP connection: %v", err)
	}

	buffer := make([]byte, len(testData))
	if _, err := conn.Read(buffer); err != nil {
		t.Fatalf("Failed to read from UDP connection: %v", err)
	}

	if string(buffer) != testData {
		t.Errorf("Expected %q, got %q", testData, string(buffer))
	}

	serverCancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("Server shutdown timeout")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		return done
	}():
	}
}

func TestTCPEchoMultipleConnections(t *testing.T) {
	originalAddrs := listenAddrs
	defer func() { listenAddrs = originalAddrs }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findAvailablePort()
	listenAddrs = []string{fmt.Sprintf("127.0.0.1:%d", port)}

	var wg sync.WaitGroup
	wg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer wg.Done()
		serveTCP(serverCtx)
	}()

	time.Sleep(300 * time.Millisecond)

	numConnections := 3
	var clientWg sync.WaitGroup
	clientWg.Add(numConnections)

	for i := range numConnections {
		go func(clientID int) {
			defer clientWg.Done()

			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
			if err != nil {
				t.Errorf("Client %d failed to connect: %v", clientID, err)
				return
			}
			defer conn.Close()

			conn.SetDeadline(time.Now().Add(time.Second))

			testData := fmt.Sprintf("Client %d", clientID)
			if _, err := conn.Write([]byte(testData)); err != nil {
				t.Errorf("Client %d failed to write: %v", clientID, err)
				return
			}

			buffer := make([]byte, len(testData))
			if _, err := conn.Read(buffer); err != nil {
				t.Errorf("Client %d failed to read: %v", clientID, err)
				return
			}

			if string(buffer) != testData {
				t.Errorf("Client %d: expected %q, got %q", clientID, testData, string(buffer))
			}
		}(i)
	}

	clientWg.Wait()
	serverCancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("Server shutdown timeout")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		return done
	}():
	}
}

func TestUDPEchoMultiplePackets(t *testing.T) {
	originalAddrs := listenAddrs
	defer func() { listenAddrs = originalAddrs }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	port := findAvailablePort()
	listenAddrs = []string{fmt.Sprintf("127.0.0.1:%d", port)}

	var wg sync.WaitGroup
	wg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer wg.Done()
		serveUDP(serverCtx)
	}()

	time.Sleep(300 * time.Millisecond)

	conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		serverCancel()
		select {
		case <-time.After(time.Second):
			t.Log("Server shutdown timeout")
		case <-func() <-chan struct{} {
			done := make(chan struct{})
			go func() { wg.Wait(); close(done) }()
			return done
		}():
		}
		t.Fatalf("Failed to connect to UDP server: %v", err)
	}
	defer conn.Close()

	numPackets := 5
	for i := range numPackets {
		conn.SetDeadline(time.Now().Add(time.Second))
		
		testData := fmt.Sprintf("Packet %d", i)
		if _, err := conn.Write([]byte(testData)); err != nil {
			t.Errorf("Failed to write packet %d: %v", i, err)
			continue
		}

		buffer := make([]byte, len(testData))
		if _, err := conn.Read(buffer); err != nil {
			t.Errorf("Failed to read packet %d: %v", i, err)
			continue
		}

		if string(buffer) != testData {
			t.Errorf("Packet %d: expected %q, got %q", i, testData, string(buffer))
		}
	}

	serverCancel()
	select {
	case <-time.After(time.Second):
		t.Fatal("Server shutdown timeout")
	case <-func() <-chan struct{} {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		return done
	}():
	}
}

func TestHandleConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	var serverConn net.Conn
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		var err error
		serverConn, err = listener.Accept()
		if err != nil {
			t.Errorf("Failed to accept connection: %v", err)
			return
		}

		if err := handleConnection(serverConn); err != nil {
			t.Logf("handleConnection finished: %v", err)
		}
	}()

	clientConn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	testMessages := []string{
		"Hello",
		"Test",
	}

	for _, msg := range testMessages {
		clientConn.SetDeadline(time.Now().Add(time.Second))
		
		if _, err := clientConn.Write([]byte(msg)); err != nil {
			t.Errorf("Failed to write message %q: %v", msg, err)
			continue
		}

		buffer := make([]byte, len(msg))
		if _, err := clientConn.Read(buffer); err != nil {
			t.Errorf("Failed to read echo for %q: %v", msg, err)
			continue
		}

		if string(buffer) != msg {
			t.Errorf("Expected echo %q, got %q", msg, string(buffer))
		}
	}

	clientConn.Close()
	wg.Wait()
}

func findAvailablePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 9100
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}