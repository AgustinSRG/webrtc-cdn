// Connection handler

package main

import (
	"github.com/gorilla/websocket"
)

type Connection_Handler struct {
	id         uint64
	ip         string
	node       *WebRTC_CDN_Node
	connection *websocket.Conn
}

func (h *Connection_Handler) run() {
	defer func() {
		h.log("Connection closed.")
		h.connection.Close()
	}()

	c := h.connection

	h.log("Connection established.")

	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			break // Closed
		}

		h.logDebug("recv: " + string(message))
		err = c.WriteMessage(mt, message)
		if err != nil {
			break // Connection broken, close it
		}
	}
}

func (h *Connection_Handler) log(msg string) {
	LogRequest(h.id, h.ip, msg)
}

func (h *Connection_Handler) logDebug(msg string) {
	LogDebugSession(h.id, h.ip, msg)
}
