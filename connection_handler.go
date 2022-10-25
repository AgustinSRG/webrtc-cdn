// Connection handler

package main

import (
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Period to send HEARTBEAT messages to the client
const HEARTBEAT_MSG_PERIOD_SECONDS = 30

// Max time with no HEARTBEAT messages to consider the connection dead
const HEARTBEAT_TIMEOUT_MS = 2 * HEARTBEAT_MSG_PERIOD_SECONDS * 1000

// Request types
const REQUEST_TYPE_PUBLISH = 1
const REQUEST_TYPE_PLAY = 2

// Connection_Handler - Stores status data
// of an active connection
type Connection_Handler struct {
	id uint64 // Connection ID
	ip string // Client IP address

	node       *WebRTC_CDN_Node // Reference to the node
	connection *websocket.Conn  // Reference to the websocket connection

	lastHeartbeat int64 // Timestamp: Last time a HEARTBEAT message was received

	closed bool // True if the connection is closed

	sendingMutex *sync.Mutex // Mutex to control sending messages
	statusMutex  *sync.Mutex // Mutex to control access to the status data

	requests     map[string]int // List of requests
	requestCount uint32         // Request count

	sources map[string]*WRTC_Source // References to associated WebRTCs sources
	sinks   map[string]*WRTC_Sink   // References to associated WebRTC sinks
}

// Initialize
func (h *Connection_Handler) init() {
	h.closed = false
	h.sendingMutex = &sync.Mutex{}
	h.statusMutex = &sync.Mutex{}
	h.requestCount = 0
	h.requests = make(map[string]int)
	h.sources = make(map[string]*WRTC_Source)
	h.sinks = make(map[string]*WRTC_Sink)
}

// Runs the handler
// Reads messages, parses them and applies them
func (h *Connection_Handler) run() {
	defer func() {
		if err := recover(); err != nil {
			switch x := err.(type) {
			case string:
				h.log("Error: " + x)
			case error:
				h.log("Error: " + x.Error())
			default:
				h.log("Connection Crashed!")
			}
		}
		h.log("Connection closed.")
		// Ensure connection is closed
		h.connection.Close()
		h.closed = true
		// Release resources
		h.onClose()
		// Remove connection
		h.node.onConnectionClose(h.id, h.ip)
	}()

	c := h.connection

	h.log("Connection established.")

	h.lastHeartbeat = time.Now().UnixMilli()
	go h.sendHeartbeatMessages() // Start heartbeat

	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			break // Closed
		}

		if mt != websocket.TextMessage {
			continue
		}

		msg := parseSignalingMessage(string(message))
		h.logDebug("Received msg: " + msg.method)

		switch msg.method {
		case "HEARTBEAT":
			h.receiveHeartbeat()
		case "PUBLISH":
			h.receivePublishMessage(msg)
		case "PLAY":
			h.receivePlayMessage(msg)
		case "ANSWER":
			h.receiveAnswerMessage(msg)
		case "CANDIDATE":
			h.receiveCandidateMessage(msg)
		case "CLOSE":
			h.receiveCloseMessage(msg)
		default:
			h.logDebug("Unknown message: " + msg.method)
		}
	}
}

// Called when a HEARTBEAT message is received from the client
func (h *Connection_Handler) receiveHeartbeat() {
	h.statusMutex.Lock()
	defer h.statusMutex.Unlock()

	h.lastHeartbeat = time.Now().UnixMilli()
}

// Checks if the client is sending HEARTBEAT messages
// If not, closes the connection
func (h *Connection_Handler) checkHeartbeat() {
	h.statusMutex.Lock()

	now := time.Now().UnixMilli()
	mustClose := (now - h.lastHeartbeat) >= HEARTBEAT_TIMEOUT_MS

	defer h.statusMutex.Unlock()

	if mustClose {
		h.connection.Close()
	}
}

// Task to send HEARTBEAT periodically
func (h *Connection_Handler) sendHeartbeatMessages() {
	for {
		time.Sleep(HEARTBEAT_MSG_PERIOD_SECONDS * time.Second)

		if h.closed {
			return // Closed
		}

		// Send heartbeat message
		msg := SignalingMessage{
			method: "HEARTBEAT",
			params: nil,
			body:   "",
		}
		h.send(msg)

		// Check heartbeat
		h.checkHeartbeat()
	}
}

// Called when a PUBLISH message is received from the client
func (h *Connection_Handler) receivePublishMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]
	streamId := msg.params["stream-id"]
	streamType := strings.ToUpper(msg.params["stream-type"])
	auth := msg.params["auth"]

	// Validate params

	if len(requestId) == 0 || len(requestId) > 255 {
		h.sendErrorMessage("INVALID_REQUEST_ID", "Request ID must be an string from 1 to 255 characters.", requestId)
		return
	}

	if len(streamId) == 0 || len(streamId) > 255 {
		h.sendErrorMessage("INVALID_STREAM_ID", "Stream ID must be an string from 1 to 255 characters.", requestId)
		return
	}

	if !checkAuthentication(auth, "stream_publish", streamId) {
		h.sendErrorMessage("INVALID_AUTH", "Invalid authentication provided.", requestId)
		return
	}

	hasAudio := true
	hasVideo := true

	if streamType == "AUDIO" {
		hasVideo = false
	} else if streamType == "VIDEO" {
		hasAudio = false
	}

	// Create source
	source := WRTC_Source{
		requestId:  requestId,
		sid:        streamId,
		node:       h.node,
		hasAudio:   hasAudio,
		hasVideo:   hasVideo,
		connection: h,
	}

	source.init()

	// Register the source
	func() {
		h.statusMutex.Lock()
		defer h.statusMutex.Unlock()

		if h.requests[requestId] != 0 {
			h.sendErrorMessage("PROTOCOL_ERROR", "You reused the same request ID for 2 different requests.", requestId)
			return
		}

		if h.requestCount > h.node.requestLimit {
			h.sendErrorMessage("LIMIT_REQUESTS", "Too many requests on the same socket.", requestId)
			return
		}

		h.requestCount++
		h.requests[requestId] = REQUEST_TYPE_PUBLISH
		h.sources[requestId] = &source

		h.sendOkMessage(requestId)

		h.node.registerSource(&source) // Register source

		go source.run() // Run source
	}()
}

// Called when a PLAY message is received from the client
func (h *Connection_Handler) receivePlayMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]
	streamId := msg.params["stream-id"]
	auth := msg.params["auth"]

	// Validate params

	if len(requestId) == 0 || len(requestId) > 255 {
		h.sendErrorMessage("INVALID_REQUEST_ID", "Request ID must be an string from 1 to 255 characters.", requestId)
		return
	}

	if len(streamId) == 0 || len(streamId) > 255 {
		h.sendErrorMessage("INVALID_STREAM_ID", "Stream ID must be an string from 1 to 255 characters.", requestId)
		return
	}

	if !checkAuthentication(auth, "stream_play", streamId) {
		h.sendErrorMessage("INVALID_AUTH", "Invalid authentication provided.", requestId)
		return
	}

	sinkId := h.node.getSinkID()

	// Create sink
	sink := WRTC_Sink{
		sinkId:     sinkId,
		requestId:  requestId,
		sid:        streamId,
		node:       h.node,
		connection: h,
	}

	sink.init()

	// Register the sink
	func() {
		h.statusMutex.Lock()
		defer h.statusMutex.Unlock()

		if h.requests[requestId] != 0 {
			h.sendErrorMessage("PROTOCOL_ERROR", "You reused the same request ID for 2 different requests.", requestId)
			return
		}

		if h.requestCount > h.node.requestLimit {
			h.sendErrorMessage("LIMIT_REQUESTS", "Too many requests on the same socket.", requestId)
			return
		}

		h.requestCount++
		h.requests[requestId] = REQUEST_TYPE_PLAY
		h.sinks[requestId] = &sink

		h.sendOkMessage(requestId)

		h.sendStandbyMessage(requestId)

		h.node.registerSink(&sink) // Register sink
	}()
}

// Called when an ANSWER message is received from the client
func (h *Connection_Handler) receiveAnswerMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]

	func() {
		h.statusMutex.Lock()
		defer h.statusMutex.Unlock()

		if h.requests[requestId] == 0 {
			return // IGNORE
		} else if h.requests[requestId] == REQUEST_TYPE_PUBLISH && h.sources[requestId] != nil {
			h.sources[requestId].onAnswer(msg.body)
		} else if h.requests[requestId] == REQUEST_TYPE_PLAY && h.sinks[requestId] != nil {
			h.sinks[requestId].onAnswer(msg.body)
		}
	}()
}

// Called when a CANDIDATE message is received from the client
func (h *Connection_Handler) receiveCandidateMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]

	func() {
		h.statusMutex.Lock()
		defer h.statusMutex.Unlock()

		if h.requests[requestId] == 0 {
			return // IGNORE
		} else if h.requests[requestId] == REQUEST_TYPE_PUBLISH {
			h.sources[requestId].onICECandidate(msg.body)
		} else if h.requests[requestId] == REQUEST_TYPE_PLAY {
			h.sinks[requestId].onICECandidate(msg.body)
		}
	}()
}

// Called when a CLOSE message is received from the client
func (h *Connection_Handler) receiveCloseMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]

	func() {
		h.statusMutex.Lock()
		defer h.statusMutex.Unlock()

		if h.requests[requestId] == 0 {
			return // IGNORE
		} else if h.requests[requestId] == REQUEST_TYPE_PUBLISH {
			h.sources[requestId].close(false, true)

			delete(h.sources, requestId)
			delete(h.requests, requestId)
			h.requestCount--
		} else if h.requests[requestId] == REQUEST_TYPE_PLAY {
			h.sinks[requestId].close()

			delete(h.sinks, requestId)
			delete(h.requests, requestId)
			h.requestCount--
		}
	}()
}

// Sends a message to the client
func (h *Connection_Handler) send(msg SignalingMessage) {
	h.sendingMutex.Lock()
	defer h.sendingMutex.Unlock()

	h.connection.WriteMessage(websocket.TextMessage, []byte(msg.serialize()))
}

// Sends an ERROR message to the client
func (h *Connection_Handler) sendErrorMessage(code string, errMsg string, requestID string) {
	msg := SignalingMessage{
		method: "ERROR",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Error-Code"] = code
	msg.params["Error-Message"] = errMsg
	msg.params["Request-ID"] = requestID

	h.send(msg)
}

// Sends an OK message to the client
func (h *Connection_Handler) sendOkMessage(requestID string) {
	msg := SignalingMessage{
		method: "OK",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Request-ID"] = requestID

	h.send(msg)
}

// Sends a STANDBY message
func (h *Connection_Handler) sendStandbyMessage(requestID string) {
	msg := SignalingMessage{
		method: "STANDBY",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Request-ID"] = requestID

	h.send(msg)
}

// Sends an OFFER message to the client
func (h *Connection_Handler) sendOffer(reqId string, sid string, offerJSON string) {
	msg := SignalingMessage{
		method: "OFFER",
		params: make(map[string]string),
		body:   offerJSON,
	}

	msg.params["Request-ID"] = reqId
	msg.params["Stream-ID"] = sid

	h.send(msg)
}

// Sends a CANDIDATE message to the client
func (h *Connection_Handler) sendICECandidate(reqId string, sid string, candidateJSON string) {
	msg := SignalingMessage{
		method: "CANDIDATE",
		params: make(map[string]string),
		body:   candidateJSON,
	}

	msg.params["Request-ID"] = reqId
	msg.params["Stream-ID"] = sid

	h.send(msg)
}

// Removes a source and send a message to the client
func (h *Connection_Handler) sendSourceClose(reqId string, sid string) {
	h.statusMutex.Lock()
	defer h.statusMutex.Unlock()

	delete(h.sources, reqId)
	delete(h.requests, reqId)
	h.requestCount--

	msg := SignalingMessage{
		method: "CLOSE",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Request-ID"] = reqId
	msg.params["Stream-ID"] = sid

	h.send(msg)
}

// Logs a message for this connection
func (h *Connection_Handler) log(msg string) {
	LogRequest(h.id, h.ip, msg)
}

// Logs a debug message for this connection
func (h *Connection_Handler) logDebug(msg string) {
	LogDebugSession(h.id, h.ip, msg)
}

// Called when connection is closed to release resources
func (h *Connection_Handler) onClose() {
	h.statusMutex.Lock()
	defer h.statusMutex.Unlock()

	for requestId := range h.requests {
		if h.requests[requestId] == 0 {
			return // IGNORE
		} else if h.requests[requestId] == REQUEST_TYPE_PUBLISH {
			h.sources[requestId].close(false, true)

			delete(h.sources, requestId)
			delete(h.requests, requestId)
			h.requestCount--
		} else if h.requests[requestId] == REQUEST_TYPE_PLAY {
			h.sinks[requestId].close()

			delete(h.sinks, requestId)
			delete(h.requests, requestId)
			h.requestCount--
		}
	}
}
