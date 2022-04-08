// Connection handler

package main

import (
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const HEARTBEAT_MSG_PERIOD_SECONDS = 30
const HEARTBEAT_TIMEOUT_MS = 2 * HEARTBEAT_MSG_PERIOD_SECONDS * 1000

const REQUEST_TYPE_PUBLISH = 1
const REQUEST_TYPE_PLAY = 2

type Connection_Handler struct {
	id uint64
	ip string

	node       *WebRTC_CDN_Node
	connection *websocket.Conn

	lastHearbeat int64

	closed bool

	sendingMutex *sync.Mutex
	statusMutex  *sync.Mutex

	requests     map[string]int
	requestCount uint32

	sources map[string]*WRTC_Source
	sinks   map[string]*WRTC_Sink
}

func (h *Connection_Handler) init() {
	h.closed = false
	h.sendingMutex = &sync.Mutex{}
	h.statusMutex = &sync.Mutex{}
	h.requestCount = 0
	h.requests = make(map[string]int)
	h.sources = make(map[string]*WRTC_Source)
	h.sinks = make(map[string]*WRTC_Sink)
}

func (h *Connection_Handler) run() {
	defer func() {
		h.log("Connection closed.")
		h.connection.Close()
		h.closed = true
		h.onClose()
		h.node.RemoveIP(h.ip)
		h.node.onClose(h.id)
	}()

	c := h.connection

	h.log("Connection established.")

	h.lastHearbeat = time.Now().UnixMilli()
	go h.sendHeartbeatMessages() // Start hearbeat

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

// HEARTBEAT

func (h *Connection_Handler) receiveHeartbeat() {
	h.statusMutex.Lock()
	defer h.statusMutex.Unlock()

	h.lastHearbeat = time.Now().UnixMilli()
}

func (h *Connection_Handler) checkHeartbeat() {
	h.statusMutex.Lock()

	now := time.Now().UnixMilli()
	mustClose := (now - h.lastHearbeat) >= HEARTBEAT_TIMEOUT_MS

	defer h.statusMutex.Unlock()

	if mustClose {
		h.connection.Close()
	}
}

func (h *Connection_Handler) sendHeartbeatMessages() {
	for {
		time.Sleep(HEARTBEAT_MSG_PERIOD_SECONDS * time.Second)

		if h.closed {
			return // Closed
		}

		// Send hearbeat message
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

// PUBLISH

func (h *Connection_Handler) receivePublishMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]
	streamId := msg.params["stream-id"]
	streamType := strings.ToUpper(msg.params["stream-type"])

	if len(requestId) == 0 || len(requestId) > 255 {
		h.sendErrorMessage("INVALID_REQUEST_ID", "Request ID must be an string from 1 to 255 characters.")
		return
	}

	if len(streamId) == 0 || len(streamId) > 255 {
		h.sendErrorMessage("INVALID_STREAM_ID", "Stream ID must be an string from 1 to 255 characters.")
		return
	}

	hasAudio := true
	hasVideo := true

	if streamType == "AUDIO" {
		hasVideo = false
	} else if streamType == "VIDEO" {
		hasAudio = false
	}

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
			h.sendErrorMessage("PROTOCOL_ERROR", "You reused the same request ID for 2 different requests.")
			return
		}

		if h.requestCount > h.node.requestLimit {
			h.sendErrorMessage("LIMIT_REQUESTS", "Too many requests on the same socket.")
			return
		}

		h.requestCount++
		h.requests[requestId] = REQUEST_TYPE_PUBLISH
		h.sources[requestId] = &source

		h.sendOkMessage(requestId)

		go source.run()
	}()
}

// PLAY

func (h *Connection_Handler) receivePlayMessage(msg SignalingMessage) {
	requestId := msg.params["request-id"]
	streamId := msg.params["stream-id"]

	if len(requestId) == 0 || len(requestId) > 255 {
		h.sendErrorMessage("INVALID_REQUEST_ID", "Request ID must be an string from 1 to 255 characters.")
		return
	}

	if len(streamId) == 0 || len(streamId) > 255 {
		h.sendErrorMessage("INVALID_STREAM_ID", "Stream ID must be an string from 1 to 255 characters.")
		return
	}

	sinkId := h.node.getSinkID()

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
			h.sendErrorMessage("PROTOCOL_ERROR", "You reused the same request ID for 2 different requests.")
			return
		}

		if h.requestCount > h.node.requestLimit {
			h.sendErrorMessage("LIMIT_REQUESTS", "Too many requests on the same socket.")
			return
		}

		h.requestCount++
		h.requests[requestId] = REQUEST_TYPE_PLAY
		h.sinks[requestId] = &sink

		h.sendOkMessage(requestId)

		go sink.run()
	}()
}

// ANSWER

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

// CANDIDATE

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

// CLOSE

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

// SEND

func (h *Connection_Handler) send(msg SignalingMessage) {
	h.sendingMutex.Lock()
	defer h.sendingMutex.Unlock()

	h.connection.WriteMessage(websocket.TextMessage, []byte(msg.serialize()))
}

func (h *Connection_Handler) sendErrorMessage(code string, errMsg string) {
	msg := SignalingMessage{
		method: "ERROR",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Error-Code"] = code
	msg.params["Error-Message"] = errMsg

	h.send(msg)
}

func (h *Connection_Handler) sendOkMessage(requestID string) {
	msg := SignalingMessage{
		method: "OK",
		params: make(map[string]string),
		body:   "",
	}

	msg.params["Request-ID"] = requestID

	h.send(msg)
}

// LOG

func (h *Connection_Handler) log(msg string) {
	LogRequest(h.id, h.ip, msg)
}

func (h *Connection_Handler) logDebug(msg string) {
	LogDebugSession(h.id, h.ip, msg)
}

// OFFER

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

// SEND CANDIDATE

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

// SEND CLOSE

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
