package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

// JTT808MessageHandler handles parsed JTT808 messages and generates responses
type JTT808MessageHandler struct {
	// Store terminal authentication status
	authenticatedTerminals map[string]bool
	// Store terminal registration info
	registeredTerminals map[string]*TerminalRegisterInfo

	// RX/TX Channels for JTT808 message processing
	rxChan chan JTT808RxMessage
	txChan chan JTT808TxMessage

	// Control channels
	running    bool
	runningMux sync.RWMutex

	// TCP server reference for sending data
	server *TCPServer

	// JTT808 parsers for each client
	clientParsers map[*Client]*JTT808Parser
	parserMutex   sync.RWMutex
}

// JTT808RxMessage represents a received JTT808 message
type JTT808RxMessage struct {
	Client    *Client
	Message   *JTT808Message
	RawData   []byte
	Timestamp time.Time
}

// JTT808TxMessage represents a message to be transmitted
type JTT808TxMessage struct {
	Client    *Client
	Data      []byte
	MessageID uint16
	Timestamp time.Time
}

// ResponseResult represents the result of message processing
type ResponseResult struct {
	Success     bool
	Code        uint8
	Description string
}

// Common response codes
const (
	RESULT_SUCCESS       = 0x00
	RESULT_FAILURE       = 0x01
	RESULT_MESSAGE_ERROR = 0x02
	RESULT_NOT_SUPPORTED = 0x03
	RESULT_ALARM_CONFIRM = 0x04
)

// NewJTT808MessageHandler creates a new message handler with RX/TX channels
func NewJTT808MessageHandler() *JTT808MessageHandler {
	return &JTT808MessageHandler{
		authenticatedTerminals: make(map[string]bool),
		registeredTerminals:    make(map[string]*TerminalRegisterInfo),
		rxChan:                 make(chan JTT808RxMessage, 100),
		txChan:                 make(chan JTT808TxMessage, 100),
		running:                false,
		clientParsers:          make(map[*Client]*JTT808Parser),
	}
}

// SetTCPServer sets the TCP server reference and configures event handlers
func (h *JTT808MessageHandler) SetTCPServer(server *TCPServer) {
	h.server = server

	// Configure TCP server event handlers for JTT808
	server.SetOnConnect(h.onClientConnect)
	server.SetOnDisconnect(h.onClientDisconnect)
	server.SetOnMessage(h.onClientMessage)
}

// Start starts the RX/TX processing goroutines
func (h *JTT808MessageHandler) Start() {
	h.setRunning(true)

	// Start RX processing goroutine
	go h.processRx()

	// Start TX processing goroutine
	go h.processTx()

	log.Println("JTT808 RX/TX processing started")
}

// Stop stops the RX/TX processing
func (h *JTT808MessageHandler) Stop() {
	h.setRunning(false)

	// Close channels
	close(h.rxChan)
	close(h.txChan)

	log.Println("JTT808 RX/TX processing stopped")
}

// IsRunning returns whether the handler is running
func (h *JTT808MessageHandler) IsRunning() bool {
	h.runningMux.RLock()
	defer h.runningMux.RUnlock()
	return h.running
}

// setRunning sets the running state thread-safely
func (h *JTT808MessageHandler) setRunning(running bool) {
	h.runningMux.Lock()
	defer h.runningMux.Unlock()
	h.running = running
}

// GetRxChannel returns the receive channel for incoming JTT808 messages
func (h *JTT808MessageHandler) GetRxChannel() <-chan JTT808RxMessage {
	return h.rxChan
}

// SendToClient sends JTT808 data to a specific client (via TX channel)
func (h *JTT808MessageHandler) SendToClient(client *Client, data []byte, messageID uint16) error {
	if !h.IsRunning() {
		return fmt.Errorf("JTT808 handler is not running")
	}

	select {
	case h.txChan <- JTT808TxMessage{
		Client:    client,
		Data:      data,
		MessageID: messageID,
		Timestamp: time.Now(),
	}:
		return nil
	default:
		return fmt.Errorf("JTT808 TX channel is full")
	}
}

// ProcessMessage processes incoming JTT808 data and adds to RX channel
func (h *JTT808MessageHandler) ProcessMessage(client *Client, message *JTT808Message, rawData []byte) {
	if !h.IsRunning() {
		log.Printf("JTT808 handler not running, dropping message from %s", client.GetID())
		return
	}

	select {
	case h.rxChan <- JTT808RxMessage{
		Client:    client,
		Message:   message,
		RawData:   rawData,
		Timestamp: time.Now(),
	}:
		// Message queued successfully
	default:
		log.Printf("JTT808 RX channel full, dropping message from client %s", client.GetID())
	}
}

// processRx processes incoming JTT808 messages from the rx channel
func (h *JTT808MessageHandler) processRx() {
	for rxMsg := range h.rxChan {
		log.Printf("JTT808 RX from %s: %s (%d bytes)",
			rxMsg.Client.GetID(), rxMsg.Message.String(), len(rxMsg.RawData))

		// Handle the message and get response
		response := h.HandleMessage(rxMsg.Message, rxMsg.Client)

		// Send response if one was generated
		if response != nil {
			err := h.SendToClient(rxMsg.Client, response, MSG_PLATFORM_GENERAL_RESP)
			if err != nil {
				log.Printf("Error queuing JTT808 response to %s: %v", rxMsg.Client.GetID(), err)
			}
		}
	}
}

// processTx processes outgoing JTT808 messages from the tx channel
func (h *JTT808MessageHandler) processTx() {
	for txMsg := range h.txChan {
		if h.server == nil {
			log.Printf("No TCP server reference, cannot send JTT808 message to %s", txMsg.Client.GetID())
			continue
		}

		// Log TX raw data before sending
		log.Printf("TX %d bytes to JTT808 client %s", len(txMsg.Data), txMsg.Client.GetID())
		log.Printf("TX Raw TCP message (hex): %s", hex.EncodeToString(txMsg.Data))

		// Send via TCP server
		err := h.server.SendToClientDirect(txMsg.Client, txMsg.Data)
		if err != nil {
			log.Printf("Error sending JTT808 message to %s: %v", txMsg.Client.GetID(), err)
		} else {
			log.Printf("JTT808 TX to %s: sent %d bytes (MsgID: 0x%04X)",
				txMsg.Client.GetID(), len(txMsg.Data), txMsg.MessageID)
		}
	}
}

// HandleMessage processes a JTT808 message and returns response data if needed
func (h *JTT808MessageHandler) HandleMessage(msg *JTT808Message, client *Client) []byte {
	log.Printf("Processing JTT808 message: %s from %s", msg.String(), client.GetID())

	switch msg.MessageID {
	case MSG_TERMINAL_REGISTER:
		return h.handleTerminalRegister(msg, client)
	case MSG_TERMINAL_AUTH:
		return h.handleTerminalAuth(msg, client)
	case MSG_TERMINAL_HEARTBEAT:
		return h.handleTerminalHeartbeat(msg, client)
	case MSG_LOCATION_REPORT:
		return h.handleLocationReport(msg, client)
	case MSG_LOCATION_BATCH_REPORT:
		return h.handleLocationBatchReport(msg, client)
	case MSG_TERMINAL_LOGOUT:
		return h.handleTerminalLogout(msg, client)
	default:
		log.Printf("Unsupported message type: 0x%04X", msg.MessageID)
		return h.createGeneralResponse(msg, RESULT_NOT_SUPPORTED)
	}
}

// handleTerminalRegister processes terminal registration
func (h *JTT808MessageHandler) handleTerminalRegister(msg *JTT808Message, client *Client) []byte {
	parser := NewJTT808Parser()
	registerInfo, err := parser.ParseTerminalRegisterInfo(msg.Body)
	if err != nil {
		log.Printf("Failed to parse register info: %v", err)
		return h.createGeneralResponse(msg, RESULT_MESSAGE_ERROR)
	}

	log.Printf("Terminal registration: Phone=%s, Manufacturer=%s, Model=%s, ID=%s, Plate=%s",
		msg.PhoneNumber, registerInfo.ManufacturerID, registerInfo.TerminalModel,
		registerInfo.TerminalID, registerInfo.LicensePlate)

	// Store registration info
	h.registeredTerminals[msg.PhoneNumber] = registerInfo

	// Generate authentication code (simplified - in production use proper algorithm)
	authCode := fmt.Sprintf("AUTH_%s_%d", msg.PhoneNumber, time.Now().Unix()%10000)

	// Create register response (0x8100)
	return h.createRegisterResponse(msg, RESULT_SUCCESS, authCode)
}

// handleTerminalAuth processes terminal authentication
func (h *JTT808MessageHandler) handleTerminalAuth(msg *JTT808Message, client *Client) []byte {
	authCode := string(msg.Body)
	log.Printf("Terminal authentication: Phone=%s, AuthCode=%s", msg.PhoneNumber, authCode)

	// Validate authentication code (simplified validation)
	if len(authCode) > 0 && len(authCode) < 50 {
		h.authenticatedTerminals[msg.PhoneNumber] = true
		log.Printf("Terminal %s authenticated successfully", msg.PhoneNumber)
		return h.createGeneralResponse(msg, RESULT_SUCCESS)
	}

	log.Printf("Terminal %s authentication failed", msg.PhoneNumber)
	return h.createGeneralResponse(msg, RESULT_FAILURE)
}

// handleTerminalHeartbeat processes terminal heartbeat
func (h *JTT808MessageHandler) handleTerminalHeartbeat(msg *JTT808Message, client *Client) []byte {
	log.Printf("Heartbeat from terminal %s", msg.PhoneNumber)

	// Check if terminal is authenticated
	if !h.authenticatedTerminals[msg.PhoneNumber] {
		log.Printf("Heartbeat from unauthenticated terminal: %s", msg.PhoneNumber)
		return h.createGeneralResponse(msg, RESULT_FAILURE)
	}

	return h.createGeneralResponse(msg, RESULT_SUCCESS)
}

// handleLocationReport processes location report
func (h *JTT808MessageHandler) handleLocationReport(msg *JTT808Message, client *Client) []byte {
	parser := NewJTT808Parser()
	location, err := parser.ParseLocationInfo(msg.Body)
	if err != nil {
		log.Printf("Failed to parse location info: %v", err)
		return h.createGeneralResponse(msg, RESULT_MESSAGE_ERROR)
	}

	// Convert coordinates to decimal degrees
	lat := float64(location.Latitude) / 1000000.0
	lng := float64(location.Longitude) / 1000000.0
	speed := float64(location.Speed) / 10.0 // Convert to km/h

	log.Printf("Location report from %s: Lat=%.6f, Lng=%.6f, Speed=%.1fkm/h, Direction=%d°, Time=%s",
		msg.PhoneNumber, lat, lng, speed, location.Direction, location.Timestamp.Format("2006-01-02 15:04:05"))

	// Log alarm and status flags if present
	if location.AlarmFlag != 0 {
		log.Printf("ALARM flags for %s: 0x%08X", msg.PhoneNumber, location.AlarmFlag)
	}

	if location.StatusFlag != 0 {
		log.Printf("Status flags for %s: 0x%08X", msg.PhoneNumber, location.StatusFlag)
	}

	return h.createGeneralResponse(msg, RESULT_SUCCESS)
}

// handleLocationBatchReport processes batch location report
func (h *JTT808MessageHandler) handleLocationBatchReport(msg *JTT808Message, client *Client) []byte {
	if len(msg.Body) < 4 {
		return h.createGeneralResponse(msg, RESULT_MESSAGE_ERROR)
	}

	// Parse batch header
	reader := bytes.NewReader(msg.Body)
	var itemCount uint16
	var locationType uint8

	binary.Read(reader, binary.BigEndian, &itemCount)
	binary.Read(reader, binary.BigEndian, &locationType)

	log.Printf("Batch location report from %s: %d items, type=%d", msg.PhoneNumber, itemCount, locationType)

	// Parse each location item (simplified - would need proper parsing based on type)
	parser := NewJTT808Parser()
	for i := uint16(0); i < itemCount && reader.Len() >= 28; i++ {
		// Read location data length (2 bytes)
		var itemLength uint16
		if err := binary.Read(reader, binary.BigEndian, &itemLength); err != nil {
			break
		}

		if reader.Len() < int(itemLength) {
			break
		}

		// Read location data
		locationData := make([]byte, itemLength)
		reader.Read(locationData)

		location, err := parser.ParseLocationInfo(locationData)
		if err != nil {
			log.Printf("Failed to parse batch location item %d: %v", i, err)
			continue
		}

		lat := float64(location.Latitude) / 1000000.0
		lng := float64(location.Longitude) / 1000000.0
		log.Printf("Batch item %d: Lat=%.6f, Lng=%.6f, Time=%s",
			i, lat, lng, location.Timestamp.Format("2006-01-02 15:04:05"))
	}

	return h.createGeneralResponse(msg, RESULT_SUCCESS)
}

// handleTerminalLogout processes terminal logout
func (h *JTT808MessageHandler) handleTerminalLogout(msg *JTT808Message, client *Client) []byte {
	log.Printf("Terminal logout: %s", msg.PhoneNumber)

	// Remove from authenticated terminals
	delete(h.authenticatedTerminals, msg.PhoneNumber)

	return h.createGeneralResponse(msg, RESULT_SUCCESS)
}

// createGeneralResponse creates a general platform response (0x8001)
func (h *JTT808MessageHandler) createGeneralResponse(originalMsg *JTT808Message, result uint8) []byte {
	// Response body: Serial number (2 bytes) + Message ID (2 bytes) + Result (1 byte)
	body := make([]byte, 5)
	binary.BigEndian.PutUint16(body[0:2], originalMsg.SerialNumber)
	binary.BigEndian.PutUint16(body[2:4], originalMsg.MessageID)
	body[4] = result

	return h.createMessage(MSG_PLATFORM_GENERAL_RESP, originalMsg.PhoneNumber, body)
}

// createRegisterResponse creates a terminal register response (0x8100)
func (h *JTT808MessageHandler) createRegisterResponse(originalMsg *JTT808Message, result uint8, authCode string) []byte {
	// Response body: Serial number (2 bytes) + Result (1 byte) + Auth code (variable)
	authBytes := []byte(authCode)
	body := make([]byte, 3+len(authBytes))

	binary.BigEndian.PutUint16(body[0:2], originalMsg.SerialNumber)
	body[2] = result
	copy(body[3:], authBytes)

	return h.createMessage(MSG_PLATFORM_REGISTER_RESP, originalMsg.PhoneNumber, body)
}

// createMessage creates a complete JTT808 message with proper framing
func (h *JTT808MessageHandler) createMessage(messageID uint16, phoneNumber string, body []byte) []byte {
	var buffer bytes.Buffer

	// Start flag
	buffer.WriteByte(JTT808_FLAG)

	// Message header
	var header bytes.Buffer

	// Message ID (2 bytes)
	binary.Write(&header, binary.BigEndian, messageID)

	// Message properties (2 bytes) - body length in lower 10 bits
	properties := uint16(len(body)) & PROP_LENGTH
	binary.Write(&header, binary.BigEndian, properties)

	// Phone number (6 bytes BCD)
	phoneBytes := h.stringToBCD(phoneNumber, 6)
	header.Write(phoneBytes)

	// Serial number (2 bytes) - use current timestamp for uniqueness
	serialNum := uint16(time.Now().Unix() % 65536)
	binary.Write(&header, binary.BigEndian, serialNum)

	// Combine header and body
	messageData := append(header.Bytes(), body...)

	// Calculate checksum
	checksum := h.calculateChecksum(messageData)
	messageData = append(messageData, checksum)

	// Escape the message data
	escapedData := h.escape(messageData)
	buffer.Write(escapedData)

	// End flag
	buffer.WriteByte(JTT808_FLAG)

	return buffer.Bytes()
}

// stringToBCD converts string to BCD format with specified length
func (h *JTT808MessageHandler) stringToBCD(str string, length int) []byte {
	result := make([]byte, length)

	// Pad with zeros if string is shorter
	paddedStr := str
	for len(paddedStr) < length*2 {
		paddedStr = "0" + paddedStr
	}

	// Convert to BCD
	for i := 0; i < length; i++ {
		if i*2+1 < len(paddedStr) {
			high := paddedStr[i*2] - '0'
			low := paddedStr[i*2+1] - '0'
			if high > 9 {
				high = 0
			}
			if low > 9 {
				low = 0
			}
			result[i] = (high << 4) | low
		}
	}

	return result
}

// escape applies JTT808 escape sequences
func (h *JTT808MessageHandler) escape(data []byte) []byte {
	var result bytes.Buffer

	for _, b := range data {
		switch b {
		case JTT808_FLAG:
			result.WriteByte(JTT808_ESCAPE)
			result.WriteByte(JTT808_ESCAPE_FLAG)
		case JTT808_ESCAPE:
			result.WriteByte(JTT808_ESCAPE)
			result.WriteByte(JTT808_ESCAPE_ESCAPE)
		default:
			result.WriteByte(b)
		}
	}

	return result.Bytes()
}

// calculateChecksum calculates XOR checksum
func (h *JTT808MessageHandler) calculateChecksum(data []byte) uint8 {
	var checksum uint8
	for _, b := range data {
		checksum ^= b
	}
	return checksum
}

// IsTerminalAuthenticated checks if a terminal is authenticated
func (h *JTT808MessageHandler) IsTerminalAuthenticated(phoneNumber string) bool {
	return h.authenticatedTerminals[phoneNumber]
}

// GetRegisteredTerminal returns registration info for a terminal
func (h *JTT808MessageHandler) GetRegisteredTerminal(phoneNumber string) *TerminalRegisterInfo {
	return h.registeredTerminals[phoneNumber]
}

// GetAuthenticatedTerminals returns list of authenticated terminals
func (h *JTT808MessageHandler) GetAuthenticatedTerminals() []string {
	terminals := make([]string, 0, len(h.authenticatedTerminals))
	for phone := range h.authenticatedTerminals {
		terminals = append(terminals, phone)
	}
	return terminals
}

// onClientConnect handles new client connections for JTT808
func (h *JTT808MessageHandler) onClientConnect(client *Client) {
	log.Printf("New JTT808 client connected: %s from %s", client.GetID(), client.GetRemoteAddr())

	// Create a parser for this client
	h.parserMutex.Lock()
	h.clientParsers[client] = NewJTT808Parser()
	h.parserMutex.Unlock()

	log.Printf("JTT808 parser initialized for client %s", client.GetID())
}

// onClientDisconnect handles client disconnections for JTT808
func (h *JTT808MessageHandler) onClientDisconnect(client *Client) {
	log.Printf("JTT808 client disconnected: %s", client.GetID())

	// Clean up parser for this client
	h.parserMutex.Lock()
	delete(h.clientParsers, client)
	h.parserMutex.Unlock()
}

// onClientMessage handles incoming messages for JTT808 processing
func (h *JTT808MessageHandler) onClientMessage(client *Client, data []byte) {
	log.Printf("RX %d bytes from JTT808 client %s", len(data), client.GetID())
	log.Printf("RX Raw TCP message (hex): %s", hex.EncodeToString(data))

	// Get parser for this client
	h.parserMutex.RLock()
	parser, exists := h.clientParsers[client]
	h.parserMutex.RUnlock()

	if !exists {
		log.Printf("No parser found for client %s", client.GetID())
		return
	}

	// Parse JTT808 messages
	messages, err := parser.Parse(data)
	if err != nil {
		log.Printf("Error parsing JTT808 data from %s: %v", client.GetID(), err)
		log.Printf("Failed to parse hex data: %s", hex.EncodeToString(data))
		return
	}

	// Process each parsed message through the handler's RX channel
	for _, msg := range messages {
		if msg == nil {
			continue
		}

		// Log the parsed message details
		log.Printf("📨 Parsed JTT808 Message: %s", msg.String())
		log.Printf("   Message ID: 0x%04X (%s)", msg.MessageID, parser.GetMessageTypeName(msg.MessageID))
		log.Printf("   Phone Number: %s", msg.PhoneNumber)
		log.Printf("   Serial Number: %d", msg.SerialNumber)
		log.Printf("   Body Length: %d bytes", msg.GetBodyLength())

		// Parse specific message types for detailed info
		switch msg.MessageID {
		case MSG_TERMINAL_REGISTER:
			if regInfo, err := parser.ParseTerminalRegisterInfo(msg.Body); err == nil {
				log.Printf("   🚗 Terminal Registration Details:")
				log.Printf("      Manufacturer: %s", regInfo.ManufacturerID)
				log.Printf("      Model: %s", regInfo.TerminalModel)
				log.Printf("      Terminal ID: %s", regInfo.TerminalID)
				log.Printf("      License Plate: %s (Color: %d)", regInfo.LicensePlate, regInfo.LicenseColor)
				log.Printf("      Province/City: %04X/%04X", regInfo.ProvinceID, regInfo.CityID)
			}
		case MSG_LOCATION_REPORT:
			if locInfo, err := parser.ParseLocationInfo(msg.Body); err == nil {
				lat := float64(locInfo.Latitude) / 1000000.0
				lng := float64(locInfo.Longitude) / 1000000.0
				speed := float64(locInfo.Speed) / 10.0
				log.Printf("   📍 Location Report Details:")
				log.Printf("      Position: %.6f°N, %.6f°E", lat, lng)
				log.Printf("      Speed: %.1f km/h, Direction: %d°", speed, locInfo.Direction)
				log.Printf("      Altitude: %d m", locInfo.Altitude)
				log.Printf("      GPS Time: %s", locInfo.Timestamp.Format("2006-01-02 15:04:05"))
				if locInfo.AlarmFlag != 0 {
					log.Printf("      ⚠️ Alarm Flags: 0x%08X", locInfo.AlarmFlag)
				}
			}
		case MSG_TERMINAL_AUTH:
			log.Printf("   🔐 Authentication Code: %s", string(msg.Body))
		case MSG_TERMINAL_HEARTBEAT:
			log.Printf("   💓 Heartbeat from terminal")
		}

		// Send to JTT808 handler's RX channel for processing
		h.ProcessMessage(client, msg, data)
	}
}

// StartJTT808Server creates and starts a complete JTT808 server
func (h *JTT808MessageHandler) StartJTT808Server(address string, port int) error {
	// Create TCP server
	h.server = NewTCPServer(address, port)

	// Configure event handlers
	h.SetTCPServer(h.server)

	// Start JTT808 processing
	h.Start()

	// Start TCP server
	if err := h.server.Start(); err != nil {
		return fmt.Errorf("failed to start TCP server: %w", err)
	}

	// Handle incoming JTT808 messages from RX channel
	go func() {
		for rxMsg := range h.GetRxChannel() {
			// Custom processing of received JTT808 messages
			log.Printf("Custom RX processing for %s: %s",
				rxMsg.Client.GetID(), rxMsg.Message.String())

			// Example: Log location data for location reports
			if rxMsg.Message.MessageID == MSG_LOCATION_REPORT {
				parser := NewJTT808Parser()
				if location, err := parser.ParseLocationInfo(rxMsg.Message.Body); err == nil {
					lat := float64(location.Latitude) / 1000000.0
					lng := float64(location.Longitude) / 1000000.0
					log.Printf("Location update: %.6f, %.6f from %s",
						lat, lng, rxMsg.Client.GetID())
				}
			}
		}
	}()

	return nil
}

// StopJTT808Server stops the complete JTT808 server
func (h *JTT808MessageHandler) StopJTT808Server() error {
	// Stop JTT808 handler first
	h.Stop()

	// Stop TCP server
	if h.server != nil {
		return h.server.Stop()
	}

	return nil
}
