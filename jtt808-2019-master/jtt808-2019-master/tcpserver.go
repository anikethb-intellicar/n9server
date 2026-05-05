package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// TCPServer represents a TCP server for basic TCP operations
type TCPServer struct {
	address    string
	port       int
	listener   net.Listener
	clients    map[net.Conn]*Client
	clientsMux sync.RWMutex
	running    bool
	runningMux sync.RWMutex

	// Event handlers
	onConnect    func(client *Client)
	onDisconnect func(client *Client)
	onMessage    func(client *Client, data []byte)
}

// Client represents a connected client
type Client struct {
	conn     net.Conn
	id       string
	writer   *bufio.Writer
	reader   *bufio.Reader
	lastSeen time.Time
	writeMux sync.Mutex
}

// Message represents a message to be sent or received
type Message struct {
	Client *Client
	Data   []byte
}

// NewTCPServer creates a new TCP server instance
func NewTCPServer(address string, port int) *TCPServer {
	return &TCPServer{
		address: address,
		port:    port,
		clients: make(map[net.Conn]*Client),
	}
}

// SetOnConnect sets the callback for when a client connects
func (s *TCPServer) SetOnConnect(handler func(client *Client)) {
	s.onConnect = handler
}

// SetOnDisconnect sets the callback for when a client disconnects
func (s *TCPServer) SetOnDisconnect(handler func(client *Client)) {
	s.onDisconnect = handler
}

// SetOnMessage sets the callback for when a message is received
func (s *TCPServer) SetOnMessage(handler func(client *Client, data []byte)) {
	s.onMessage = handler
}

// Start starts the TCP server
func (s *TCPServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.address, s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listener = listener
	s.setRunning(true)

	log.Printf("TCP Server started on %s", addr)

	// Accept connections
	go s.acceptConnections()

	return nil
}

// Stop stops the TCP server
func (s *TCPServer) Stop() error {
	s.setRunning(false)

	if s.listener != nil {
		s.listener.Close()
	}

	// Close all client connections
	s.clientsMux.Lock()
	for conn, client := range s.clients {
		conn.Close()
		if s.onDisconnect != nil {
			s.onDisconnect(client)
		}
	}
	s.clients = make(map[net.Conn]*Client)
	s.clientsMux.Unlock()

	log.Println("TCP Server stopped")
	return nil
}

// IsRunning returns whether the server is running
func (s *TCPServer) IsRunning() bool {
	s.runningMux.RLock()
	defer s.runningMux.RUnlock()
	return s.running
}

// setRunning sets the running state thread-safely
func (s *TCPServer) setRunning(running bool) {
	s.runningMux.Lock()
	defer s.runningMux.Unlock()
	s.running = running
}

// SendToClientDirect sends data directly to a client connection
func (s *TCPServer) SendToClientDirect(client *Client, data []byte) error {
	if !s.IsRunning() {
		return fmt.Errorf("server is not running")
	}

	return s.writeToClient(client, data)
}

// Broadcast sends data to all connected clients
func (s *TCPServer) Broadcast(data []byte) {
	s.clientsMux.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.clientsMux.RUnlock()

	for _, client := range clients {
		s.SendToClientDirect(client, data)
	}
}

// GetClientCount returns the number of connected clients
func (s *TCPServer) GetClientCount() int {
	s.clientsMux.RLock()
	defer s.clientsMux.RUnlock()
	return len(s.clients)
}

// acceptConnections accepts incoming connections
func (s *TCPServer) acceptConnections() {
	for s.IsRunning() {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.IsRunning() {
				log.Printf("Error accepting connection: %v", err)
			}
			continue
		}

		client := &Client{
			conn:     conn,
			id:       fmt.Sprintf("%s-%d", conn.RemoteAddr().String(), time.Now().Unix()),
			writer:   bufio.NewWriter(conn),
			reader:   bufio.NewReader(conn),
			lastSeen: time.Now(),
		}

		s.clientsMux.Lock()
		s.clients[conn] = client
		s.clientsMux.Unlock()

		log.Printf("Client connected: %s", client.id)

		if s.onConnect != nil {
			s.onConnect(client)
		}

		// Handle client in separate goroutine
		go s.handleClient(client)
	}
}

// handleClient handles a client connection
func (s *TCPServer) handleClient(client *Client) {
	defer func() {
		client.conn.Close()

		s.clientsMux.Lock()
		delete(s.clients, client.conn)
		s.clientsMux.Unlock()

		log.Printf("Client disconnected: %s", client.id)

		if s.onDisconnect != nil {
			s.onDisconnect(client)
		}
	}()

	buffer := make([]byte, 4096)

	for s.IsRunning() {
		// Set read timeout
		client.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		n, err := client.conn.Read(buffer)
		if err != nil {
			if err == io.EOF {
				log.Printf("Client %s closed connection", client.id)
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("Read timeout for client %s", client.id)
			} else {
				log.Printf("Error reading from client %s: %v", client.id, err)
			}
			break
		}

		if n > 0 {
			client.lastSeen = time.Now()
			data := make([]byte, n)
			copy(data, buffer[:n])

			// Call message handler if set
			if s.onMessage != nil {
				s.onMessage(client, data)
			}
		}
	}
}

// writeToClient writes data to a client connection
func (s *TCPServer) writeToClient(client *Client, data []byte) error {
	client.writeMux.Lock()
	defer client.writeMux.Unlock()

	// Log TX raw data before sending
	log.Printf("TX %d bytes to JTT808 client %s", len(data), client.id)
	log.Printf("TX Raw TCP message (hex): %s", hex.EncodeToString(data))

	// Set write timeout
	client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	n, err := client.writer.Write(data)
	if err != nil {
		return err
	}

	if err := client.writer.Flush(); err != nil {
		return err
	}

	log.Printf("TX to %s: sent %d bytes", client.id, n)
	return nil
}

// GetClientID returns the client's ID
func (c *Client) GetID() string {
	return c.id
}

// GetRemoteAddr returns the client's remote address
func (c *Client) GetRemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// GetLastSeen returns when the client was last seen
func (c *Client) GetLastSeen() time.Time {
	return c.lastSeen
}

// main function - Simplified JTT808 server
func main() {
	// Create JTT808 message handler
	jttHandler := NewJTT808MessageHandler()

	// Start the complete JTT808 server
	if err := jttHandler.StartJTT808Server("0.0.0.0", 11000); err != nil {
		log.Fatalf("Failed to start JTT808 server: %v", err)
	}

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("JTT808 Server is running on port 11000. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan

	log.Println("Shutting down JTT808 server...")

	// Stop JTT808 server
	if err := jttHandler.StopJTT808Server(); err != nil {
		log.Printf("Error stopping JTT808 server: %v", err)
	}

	log.Println("JTT808 Server stopped gracefully")
}
