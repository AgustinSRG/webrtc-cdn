// WebRTC source connection.
// Receives data from a local websocket connection.
// Can send data to other local connections or to other nodes

package main

import (
	"sync"

	"github.com/pion/webrtc/v3"
)

// WRTC_Source -This data structure contains the status data
// of a source connection (Client -> Node)
// The source registers itself into the node, and the node pipes
// the data to the sinks and external data senders
type WRTC_Source struct {
	requestId string // Unique Request ID in the websocket connection
	sid       string // Stream ID being pushed

	node       *WebRTC_CDN_Node    // Node reference
	connection *Connection_Handler // Websocket connection reference

	ready bool // If true, tracks are available

	closed bool // If true, source is no longer active

	peerConnection *webrtc.PeerConnection // WebRTC Peer Connection

	statusMutex *sync.Mutex // Mutex to control access to the struct

	hasAudio        bool
	localTrackAudio *webrtc.TrackLocalStaticRTP // Audio track

	hasVideo        bool
	localTrackVideo *webrtc.TrackLocalStaticRTP // Video track

}

// Initialize
func (source *WRTC_Source) init() {
	source.closed = false
	source.ready = false
	source.statusMutex = &sync.Mutex{}
}

// Creates the connection and generates the offer
// Registers itself into the node
// Also sets the event handlers
func (source *WRTC_Source) run() {
	peerConnectionConfig := loadWebRTCConfig() // Load config

	source.statusMutex.Lock()

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		source.statusMutex.Unlock()
		LogError(err)
		source.close(true, false)
		return
	}

	source.peerConnection = peerConnection

	source.statusMutex.Unlock()

	// Register source
	source.node.registerSource(source)

	// Track event handler
	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		source.statusMutex.Lock()
		defer source.statusMutex.Unlock()

		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			// Received video track
			if source.localTrackVideo != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "video", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
				source.close(true, true)
				return
			}

			source.localTrackVideo = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			// Received audio track
			if source.localTrackAudio != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "audio", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
				source.close(true, true)
				return
			}

			source.localTrackAudio = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else {
			return
		}

		if (!source.hasAudio || source.localTrackAudio != nil) && (!source.hasVideo || source.localTrackVideo != nil) {
			// Received all the tracks
			source.node.onSourceReady(source)
		}
	})

	// ICE Candidate handler
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		source.connection.sendICECandidate(source.requestId, source.sid, i.ToJSON().Candidate)
	})

	// Connection status handler
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			source.connection.logDebug("Source Disconnected | SreamID: " + source.sid + " | RequestID: " + source.requestId)
			source.onClose() // Disconnected
		} else if state == webrtc.PeerConnectionStateConnected {
			source.connection.logDebug("Source Connected | SreamID: " + source.sid + " | RequestID: " + source.requestId)
		}
	})

	// Create transceivers

	if source.hasVideo {
		// Create transceiver to receive a VIDEO track
		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
			LogError(err)
			source.close(true, true)
			return
		}
	}

	if source.hasAudio {
		// Create transceiver to receive an AUDIO track
		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
			LogError(err)
			source.close(true, true)
			return
		}
	}

	// Generate offer
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		LogError(err)
		source.close(true, true)
		return
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		LogError(err)
		source.close(true, true)
		return
	}

	// Send to the client
	source.connection.sendOffer(source.requestId, source.sid, offer.SDP)
}

// ICE Candidate message received from the client
func (source *WRTC_Source) onICECandidate(sdp string) {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.peerConnection == nil {
		return
	}

	err := source.peerConnection.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: sdp,
	})
	if err != nil {
		LogError(err)
	}
}

// ANSWER message received from the client
func (source *WRTC_Source) onAnswer(sdp string) {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.peerConnection == nil {
		return
	}

	// Set the remote SessionDescription
	err := source.peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		LogError(err)
		return
	}
}

// CLOSE

// Peer connection closed
func (source *WRTC_Source) onClose() {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.closed {
		return
	}
	source.closed = true

	// Send close message to the connection
	source.connection.sendSourceClose(source.requestId, source.sid)

	// Deregister source
	source.node.onSourceClosed(source)
}

// Source manually closed
func (source *WRTC_Source) close(notifyConnection bool, deregister bool) {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.closed {
		return
	}

	source.closed = true

	if source.peerConnection != nil {
		// Close the peer connection
		source.peerConnection.OnConnectionStateChange(nil)
		source.peerConnection.OnICECandidate(nil)
		source.peerConnection.OnTrack(nil)
		source.peerConnection.Close()
	}

	source.peerConnection = nil

	// Send close message to the connection
	if notifyConnection {
		source.connection.sendSourceClose(source.requestId, source.sid)
	}

	// Deregister source
	if deregister {
		source.node.onSourceClosed(source)
	}
}
