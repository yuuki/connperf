package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

// testRunClientE2E wraps runClient for e2e testing
func testRunClientE2E(out io.Writer, args []string) error {
	// Save original values
	originalStdout := os.Stdout
	originalArgs := os.Args

	// Create pipe to capture output if needed
	if out != os.Stdout {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		os.Stdout = w

		// Copy output to our writer in background
		go func() {
			defer r.Close()
			io.Copy(out, r)
		}()

		defer func() {
			w.Close()
			os.Stdout = originalStdout
		}()
	}

	// Simulate command line args for client mode
	os.Args = append([]string{"tcpulse", "-c"}, args...)
	defer func() { os.Args = originalArgs }()

	// Parse flags to set the variables
	pflag.CommandLine.Parse(os.Args[1:])

	// Ensure client mode is enabled
	clientMode = true
	serverMode = false

	return runClient()
}

// TestE2ETCPServerClientEcho tests end-to-end TCP communication
func TestE2ETCPServerClientEcho(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "tcp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is listening
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		serverCancel()
		t.Fatalf("Failed to connect to server: %v", err)
	}
	conn.Close()

	// Setup client
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 1 * time.Second
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true

	// Run client
	var clientOutput bytes.Buffer

	err = testRunClientE2E(&clientOutput, []string{addr})
	if err != nil {
		t.Errorf("Client error: %v", err)
	}

	// Verify output contains expected metrics
	output := clientOutput.String()
	if !strings.Contains(output, "PEER") {
		t.Errorf("Expected header in output, got: %s", output)
	}
	if !strings.Contains(output, addr) {
		t.Errorf("Expected address in output, got: %s", output)
	}
	if !strings.Contains(output, "--- A result during total execution time ---") {
		t.Errorf("Expected final report in output, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2EUDPServerClientEcho tests end-to-end UDP communication
func TestE2EUDPServerClientEcho(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "udp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "udp",
		})
		if err := server.serveUDP(serverCtx); err != nil {
			t.Logf("UDP Server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is listening
	conn, err := net.DialTimeout("udp", addr, time.Second)
	if err != nil {
		serverCancel()
		t.Fatalf("Failed to connect to UDP server: %v", err)
	}
	conn.Close()

	// Setup client
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "udp"
	duration = 1 * time.Second
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true

	// Run client
	var clientOutput bytes.Buffer

	err = testRunClientE2E(&clientOutput, []string{addr})
	if err != nil {
		t.Errorf("UDP Client error: %v", err)
	}

	// Verify output
	output := clientOutput.String()
	if !strings.Contains(output, "PEER") {
		t.Errorf("Expected header in UDP output, got: %s", output)
	}
	if !strings.Contains(output, addr) {
		t.Errorf("Expected address in UDP output, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2ETCPPersistentMode tests persistent connection mode
func TestE2ETCPPersistentMode(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "tcp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Persistent mode server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Setup client for persistent mode
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorPersistent
	duration = 1 * time.Second
	connections = 2
	connectRate = 10
	messageBytes = 64
	showOnlyResults = true

	// Run client
	var clientOutput bytes.Buffer

	err := testRunClientE2E(&clientOutput, []string{addr})
	if err != nil {
		t.Errorf("Persistent client error: %v", err)
	}

	// Verify persistent mode specific behavior
	output := clientOutput.String()
	if !strings.Contains(output, addr) {
		t.Errorf("Expected address in persistent output, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2ETCPEphemeralMode tests ephemeral connection mode
func TestE2ETCPEphemeralMode(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "tcp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Ephemeral mode server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Setup client for ephemeral mode
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 1 * time.Second
	connectRate = 8
	messageBytes = 128
	showOnlyResults = true

	// Run client
	var clientOutput bytes.Buffer

	err := testRunClientE2E(&clientOutput, []string{addr})
	if err != nil {
		t.Errorf("Ephemeral client error: %v", err)
	}

	// Verify ephemeral mode specific behavior
	output := clientOutput.String()
	if !strings.Contains(output, addr) {
		t.Errorf("Expected address in ephemeral output, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2EMultipleAddresses tests connecting to multiple server addresses
func TestE2EMultipleAddresses(t *testing.T) {
	port1 := findAvailablePort()
	port2 := findAvailablePort()
	addr1 := fmt.Sprintf("127.0.0.1:%d", port1)
	addr2 := fmt.Sprintf("127.0.0.1:%d", port2)

	// Setup servers
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr1, addr2}
	serveProtocol = "tcp"

	// Start servers
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: listenAddrs,
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Multi-address servers completed: %v", err)
		}
	}()

	// Wait for servers to start
	time.Sleep(200 * time.Millisecond)

	// Setup client
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 1 * time.Second
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true
	mergeResultsEachHost = false

	// Run client
	var clientOutput bytes.Buffer

	err := testRunClientE2E(&clientOutput, []string{addr1, addr2})
	if err != nil {
		t.Errorf("Multi-address client error: %v", err)
	}

	// Verify both addresses are in output
	output := clientOutput.String()
	if !strings.Contains(output, addr1) {
		t.Errorf("Expected address %s in output, got: %s", addr1, output)
	}
	if !strings.Contains(output, addr2) {
		t.Errorf("Expected address %s in output, got: %s", addr2, output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2EMergedResults tests result merging functionality
func TestE2EMergedResults(t *testing.T) {
	port1 := findAvailablePort()
	port2 := findAvailablePort()
	addr1 := fmt.Sprintf("127.0.0.1:%d", port1)
	addr2 := fmt.Sprintf("127.0.0.1:%d", port2)

	// Setup servers
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr1, addr2}
	serveProtocol = "tcp"

	// Start servers
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: listenAddrs,
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Merged results servers completed: %v", err)
		}
	}()

	// Wait for servers to start
	time.Sleep(200 * time.Millisecond)

	// Setup client with result merging
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 1 * time.Second
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true
	mergeResultsEachHost = true

	// Run client
	var clientOutput bytes.Buffer

	err := testRunClientE2E(&clientOutput, []string{addr1, addr2})
	if err != nil {
		t.Errorf("Merged results client error: %v", err)
	}

	// Verify merged results format
	output := clientOutput.String()
	if !strings.Contains(output, "merged(2 hosts)") {
		t.Errorf("Expected merged results indicator, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2ELargeMessageSize tests communication with larger message sizes
func TestE2ELargeMessageSize(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "tcp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Large message server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Setup client with large message size
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 1 * time.Second
	connectRate = 3
	messageBytes = 1024 // Larger message size
	showOnlyResults = true

	// Run client
	var clientOutput bytes.Buffer

	err := testRunClientE2E(&clientOutput, []string{addr})
	if err != nil {
		t.Errorf("Large message client error: %v", err)
	}

	// Verify output
	output := clientOutput.String()
	if !strings.Contains(output, addr) {
		t.Errorf("Expected address in large message output, got: %s", output)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}

// TestE2EServerShutdown tests graceful server shutdown during client operation
func TestE2EServerShutdown(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "tcp"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(1)

	serverCtx, serverCancel := context.WithCancel(ctx)
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("Shutdown test server completed: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Setup client with longer duration
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 3 * time.Second // Longer duration
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true

	// Start client in goroutine
	var clientWg sync.WaitGroup
	clientWg.Add(1)
	var clientErr error

	go func() {
		defer clientWg.Done()
		var clientOutput bytes.Buffer
		clientErr = testRunClientE2E(&clientOutput, []string{addr})
	}()

	// Let client run for a bit, then shutdown server
	time.Sleep(500 * time.Millisecond)
	serverCancel()
	serverWg.Wait()

	// Wait for client to complete
	clientWg.Wait()

	// Client should handle server shutdown gracefully
	// (may or may not result in error depending on timing)
	t.Logf("Client completed with: %v", clientErr)
}

// Helper functions for preserving and restoring global variables
type clientVars struct {
	protocol             string
	connectFlavor        string
	duration             time.Duration
	connections          int32
	connectRate          int32
	messageBytes         int32
	showOnlyResults      bool
	mergeResultsEachHost bool
}

func preserveClientVars() clientVars {
	return clientVars{
		protocol:             protocol,
		connectFlavor:        connectFlavor,
		duration:             duration,
		connections:          connections,
		connectRate:          connectRate,
		messageBytes:         messageBytes,
		showOnlyResults:      showOnlyResults,
		mergeResultsEachHost: mergeResultsEachHost,
	}
}

func restoreClientVars(vars clientVars) {
	protocol = vars.protocol
	connectFlavor = vars.connectFlavor
	duration = vars.duration
	connections = vars.connections
	connectRate = vars.connectRate
	messageBytes = vars.messageBytes
	showOnlyResults = vars.showOnlyResults
	mergeResultsEachHost = vars.mergeResultsEachHost
}

// TestE2EAllProtocols tests server serving both TCP and UDP simultaneously
func TestE2EAllProtocols(t *testing.T) {
	port := findAvailablePort()
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Setup server for all protocols
	originalServeAddrs := listenAddrs
	originalServeProtocol := serveProtocol
	defer func() {
		listenAddrs = originalServeAddrs
		serveProtocol = originalServeProtocol
	}()

	listenAddrs = []string{addr}
	serveProtocol = "all"

	// Start server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var serverWg sync.WaitGroup
	serverWg.Add(2) // TCP and UDP servers

	serverCtx, serverCancel := context.WithCancel(ctx)

	// Start both TCP and UDP servers
	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "tcp",
		})
		if err := server.serveTCP(serverCtx); err != nil {
			t.Logf("All protocols TCP server completed: %v", err)
		}
	}()

	go func() {
		defer serverWg.Done()
		server := NewServer(ServerConfig{
			ListenAddrs: []string{addr},
			Protocol:    "udp",
		})
		if err := server.serveUDP(serverCtx); err != nil {
			t.Logf("All protocols UDP server completed: %v", err)
		}
	}()

	// Wait for servers to start
	time.Sleep(200 * time.Millisecond)

	// Test TCP client
	originalClientVars := preserveClientVars()
	defer restoreClientVars(originalClientVars)

	protocol = "tcp"
	connectFlavor = flavorEphemeral
	duration = 500 * time.Millisecond
	connectRate = 5
	messageBytes = 32
	showOnlyResults = true

	var tcpOutput bytes.Buffer

	err := testRunClientE2E(&tcpOutput, []string{addr})
	if err != nil {
		t.Errorf("TCP client error with all protocols server: %v", err)
	}

	// Test UDP client
	protocol = "udp"

	var udpOutput bytes.Buffer

	err = testRunClientE2E(&udpOutput, []string{addr})
	if err != nil {
		t.Errorf("UDP client error with all protocols server: %v", err)
	}

	// Verify both protocols worked
	tcpOut := tcpOutput.String()
	udpOut := udpOutput.String()

	if !strings.Contains(tcpOut, addr) {
		t.Errorf("Expected TCP address in output, got: %s", tcpOut)
	}
	if !strings.Contains(udpOut, addr) {
		t.Errorf("Expected UDP address in output, got: %s", udpOut)
	}

	// Cleanup
	serverCancel()
	serverWg.Wait()
}
