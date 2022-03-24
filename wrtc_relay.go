// WebRTC relay connection. Used to receive WebRTC data from another node

package main

import (
	"sync"

	"github.com/pion/webrtc/v3"
)

type WRTC_Relay struct {
	sid          string
	remoteNodeId string
	node         *WebRTC_CDN_Node

	closed bool

	peerConnection *webrtc.PeerConnection
	statusMutex    *sync.Mutex

	hasAudio bool
	hasVideo bool

	localTrackVideo *webrtc.TrackLocalStaticRTP
	localTrackAudio *webrtc.TrackLocalStaticRTP
}

func (relay *WRTC_Relay) init() {
	relay.closed = false
	relay.statusMutex = &sync.Mutex{}
}

func (relay *WRTC_Relay) run() {
	peerConnectionConfig := loadWebRTCConfig()

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		relay.onClose()
		return
	}

	relay.peerConnection = peerConnection

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		relay.statusMutex.Lock()
		defer relay.statusMutex.Unlock()

		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			if relay.localTrackVideo != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "video", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
			}

			relay.localTrackVideo = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			if relay.localTrackAudio != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "audio", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
			}

			relay.localTrackAudio = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else {
			return
		}

		if (!relay.hasAudio || relay.localTrackAudio != nil) && (!relay.hasVideo || relay.localTrackVideo != nil) {
			// Received all the tracks

		}
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		msg := make(map[string]string)
		msg["type"] = "CANDIDATE"
		msg["src"] = relay.node.id
		msg["dst"] = relay.remoteNodeId
		msg["sid"] = relay.sid
		msg["sdp"] = i.ToJSON().Candidate
		relay.node.sendRedisMessage(relay.remoteNodeId, &msg)
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			relay.onClose()
		}
	})
}

func (relay *WRTC_Relay) onICECandidate(sdp string) {
	err := relay.peerConnection.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: sdp,
	})
	if err != nil {
		LogError(err)
	}
}

func (relay *WRTC_Relay) onOffer(sdp string) {
	// Set the remote SessionDescription
	err := relay.peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	})
	if err != nil {
		LogError(err)
		return
	}

	answer, err := relay.peerConnection.CreateAnswer(nil)
	if err != nil {
		LogError(err)
		return
	}

	// Send to the signaling system
	msg := make(map[string]string)
	msg["type"] = "ANSWER"
	msg["src"] = relay.node.id
	msg["dst"] = relay.remoteNodeId
	msg["sid"] = relay.sid
	msg["sdp"] = answer.SDP
	relay.node.sendRedisMessage(relay.remoteNodeId, &msg)

	// Sets the LocalDescription, and starts our UDP listeners
	err = relay.peerConnection.SetLocalDescription(answer)
	if err != nil {
		LogError(err)
		return
	}
}

func (relay *WRTC_Relay) addConnection(con *Connection_Handler) {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()
}

func (relay *WRTC_Relay) removeConnection(id uint64) {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()
}

func (relay *WRTC_Relay) onClose() {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()

	if relay.closed {
		return
	}
	relay.closed = true

	// Remove all the linked connections
}
