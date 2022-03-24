// Connection handler

package main

import (
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
	requestCount int

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

}

// PLAY

func (h *Connection_Handler) receivePlayMessage(msg SignalingMessage) {

}

// ANSWER

func (h *Connection_Handler) receiveAnswerMessage(msg SignalingMessage) {

}

// CANDIDATE

func (h *Connection_Handler) receiveCandidateMessage(msg SignalingMessage) {

}

// CLOSE

func (h *Connection_Handler) receiveCloseMessage(msg SignalingMessage) {

}

// SEND

func (h *Connection_Handler) send(msg SignalingMessage) {
	h.sendingMutex.Lock()
	defer h.sendingMutex.Unlock()

	h.connection.WriteMessage(websocket.TextMessage, []byte(msg.serialize()))
}

// LOG

func (h *Connection_Handler) log(msg string) {
	LogRequest(h.id, h.ip, msg)
}

func (h *Connection_Handler) logDebug(msg string) {
	LogDebugSession(h.id, h.ip, msg)
}

// OFFER

func (h *Connection_Handler) sendOffer(reqId string, sid string, sdp string) {
	msg := SignalingMessage{
		method: "OFFER",
		params: make(map[string]string),
		body:   sdp,
	}

	msg.params["Request-ID"] = reqId
	msg.params["Stream-ID"] = sid

	h.send(msg)
}

// SEND CANDIDATE

func (h *Connection_Handler) sendICECandidate(reqId string, sid string, sdp string) {
	msg := SignalingMessage{
		method: "CANDIDATE",
		params: make(map[string]string),
		body:   sdp,
	}

	msg.params["Request-ID"] = reqId
	msg.params["Stream-ID"] = sid

	h.send(msg)
}
