// WebRTC relay connection. Used to receive WebRTC data from another node

package main

import (
	"encoding/json"
	"sync"

	"github.com/pion/webrtc/v3"
)

// WRTC_Relay - This data structure contains the status data
// of an inter-node INPUT connection
// Receives the tracks of a remote WRTC_Source
type WRTC_Relay struct {
	sid      string // WebRTC stream ID
	remoteId string // ID of the remote node sending it

	node *WebRTC_CDN_Node // Reference to the node

	ready bool

	peerConnection *webrtc.PeerConnection
	statusMutex    *sync.Mutex

	hasAudio bool
	hasVideo bool

	localTrackVideo *webrtc.TrackLocalStaticRTP
	localTrackAudio *webrtc.TrackLocalStaticRTP
}

// Initialize
func (relay *WRTC_Relay) init() {
	relay.ready = false
	relay.statusMutex = &sync.Mutex{}
}

// Called when an offer SDP message is received
func (relay *WRTC_Relay) onOffer(sdp string, hasVideo bool, hasAudio bool) {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()

	relay.hasVideo = hasVideo
	relay.hasAudio = hasAudio

	// Clear old peer connection
	if relay.peerConnection != nil {
		relay.peerConnection.OnICECandidate(nil)
		relay.peerConnection.OnConnectionStateChange(nil)
		relay.peerConnection.OnTrack(nil)
		relay.peerConnection.Close()
	}

	peerConnectionConfig := loadWebRTCConfig() // Load WebRTC configuration

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		go relay.onClose()
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
			// Received all the tracks, the relay is now ready
			relay.node.onRelayReady(relay)
		}
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		relay.statusMutex.Lock()
		defer relay.statusMutex.Unlock()

		if i != nil {
			b, e := json.Marshal(i.ToJSON())
			if e != nil {
				LogError(e)
			} else {
				relay.sendICECandidate(string(b))
			}
		} else {
			relay.sendICECandidate("")
		}
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			LogDebug("Source relay Disconnected | RemoteNode: " + relay.remoteId + " | SreamID: " + relay.sid)
			relay.onClose()
		} else if state == webrtc.PeerConnectionStateConnected {
			LogDebug("Source relay Connected | RemoteNode: " + relay.remoteId + " | SreamID: " + relay.sid)
		}
	})

	// Set the remote SessionDescription
	err = relay.peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	})
	if err != nil {
		LogError(err)
		return
	}

	// Create SDP answer
	answer, err := relay.peerConnection.CreateAnswer(nil)
	if err != nil {
		LogError(err)
		return
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = relay.peerConnection.SetLocalDescription(answer)
	if err != nil {
		LogError(err)
		return
	}

	// Send ANSWER to the signaling system
	relay.sendAnswer(answer.SDP)
}

// Call when an ICE Candidate message is received from the remote node
func (relay *WRTC_Relay) onICECandidate(candidateJSON string) {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()

	if relay.peerConnection == nil {
		return
	}

	if candidateJSON == "" {
		return
	}

	candidate := webrtc.ICECandidateInit{}

	err := json.Unmarshal([]byte(candidateJSON), &candidate)

	if err != nil {
		LogError(err)
	}

	err = relay.peerConnection.AddICECandidate(candidate)

	if err != nil {
		LogError(err)
	}
}

// SEND

// Send candidate message to the remote node
func (relay *WRTC_Relay) sendICECandidate(candidate string) {
	mp := make(map[string]string)

	mp["type"] = "CANDIDATE"
	mp["src"] = relay.node.id
	mp["dst"] = relay.remoteId
	mp["sid"] = relay.sid
	mp["candidate"] = candidate

	relay.node.sendRedisMessage(relay.remoteId, &mp)
}

// Send answer SDP message to the remote node
func (relay *WRTC_Relay) sendAnswer(sdp string) {
	mp := make(map[string]string)

	mp["type"] = "ANSWER"
	mp["src"] = relay.node.id
	mp["dst"] = relay.remoteId
	mp["sid"] = relay.sid
	mp["sdp"] = sdp

	relay.node.sendRedisMessage(relay.remoteId, &mp)
}

// Called if the peer connection is closed
func (relay *WRTC_Relay) onClose() {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()

	if relay.peerConnection != nil {
		relay.peerConnection.OnICECandidate(nil)
		relay.peerConnection.OnConnectionStateChange(nil)
		relay.peerConnection.OnTrack(nil)
		relay.peerConnection.Close()
	}

	relay.peerConnection = nil
	relay.hasAudio = false
	relay.hasVideo = false
	relay.localTrackAudio = nil
	relay.localTrackVideo = nil

	relay.node.onRelayClosed(relay)
}

// Close the relay
func (relay *WRTC_Relay) close() {
	relay.statusMutex.Lock()
	defer relay.statusMutex.Unlock()

	if relay.peerConnection != nil {
		relay.peerConnection.OnICECandidate(nil)
		relay.peerConnection.OnConnectionStateChange(nil)
		relay.peerConnection.OnTrack(nil)
		relay.peerConnection.Close()
	}

	relay.peerConnection = nil
	relay.hasAudio = false
	relay.hasVideo = false
	relay.localTrackAudio = nil
	relay.localTrackVideo = nil
}
