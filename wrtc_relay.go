// WebRTC relay connection. Used to receive WebRTC data from another node

package main

import (
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

const (
	rtcpPLIInterval = time.Second * 3
)

type WRTC_Relay struct {
	node *WebRTC_CDN_Node

	peerConnection *webrtc.PeerConnection
	statusMutex    *sync.Mutex

	connections map[uint64]*Connection_Handler
}

func (relay *WRTC_Relay) init() {
	relay.statusMutex = &sync.Mutex{}

	relay.connections = make(map[uint64]*Connection_Handler)
}

func (relay *WRTC_Relay) run() {

}

func (relay *WRTC_Relay) onICECandidate(sdp string) {

}

func (relay *WRTC_Relay) onOffer(sdp string) {

}

func (relay *WRTC_Relay) addConnection(con *Connection_Handler) {

}
