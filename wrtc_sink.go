// WebRTC sink connection.
// Sends data to a local websocket connection.

package main

import (
	"fmt"
	"sync"

	"github.com/pion/webrtc/v3"
)

// WRTC_Sink - This data structure contains the status data
// of a sink connection (Node -> Client)
// The sink registers itself into the node, who notifies it
// when there are new tracks available
// The sink retries the connection is it's closed
// The sink will replace the existing connection with a new one if the source is replaced
type WRTC_Sink struct {
	sinkId    uint64 // Unique ID for the sink in the node
	requestId string // Unique request ID in the associated the websocket connection
	sid       string // Requested stream ID to pull

	node       *WebRTC_CDN_Node    // Reference to the node
	connection *Connection_Handler // Reference to the websocket connection

	closed bool // True when the sink is no longer active, prevent reconnection

	peerConnection *webrtc.PeerConnection // WebRTC Peer Connection

	statusMutex *sync.Mutex // Mutex to control access to the struct

	hasAudio        bool
	localTrackAudio *webrtc.TrackLocalStaticRTP // Audio track

	hasVideo        bool
	localTrackVideo *webrtc.TrackLocalStaticRTP // Video track
}

// Initialize
func (sink *WRTC_Sink) init() {
	sink.statusMutex = &sync.Mutex{}
	sink.closed = false
}

// Registers the sink into the node to access the requested stream
func (sink *WRTC_Sink) run() {
	sink.node.registerSink(sink)
}

// Receive the tracks from local source or relay
func (sink *WRTC_Sink) onTracksReady(localTrackVideo *webrtc.TrackLocalStaticRTP, localTrackAudio *webrtc.TrackLocalStaticRTP) {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	// Set video track
	sink.localTrackVideo = localTrackVideo
	sink.hasVideo = localTrackVideo != nil

	// Set audio track
	sink.localTrackAudio = localTrackAudio
	sink.hasAudio = localTrackAudio != nil

	// If there is an existing connection, close it
	if sink.peerConnection != nil {
		sink.peerConnection.OnICECandidate(nil)
		sink.peerConnection.OnConnectionStateChange(nil)
		sink.peerConnection.Close()
	}

	sink.peerConnection = nil

	// Run the connection process
	go sink.runAfterTracksReady()
}

// Starts the peer connection, generates the offer and sets up the event handlers
func (sink *WRTC_Sink) runAfterTracksReady() {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	if !sink.hasVideo && !sink.hasAudio {
		return // Nothing to do
	}

	peerConnectionConfig := loadWebRTCConfig() // Load config

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		return
	}

	sink.peerConnection = peerConnection

	// ICE candidate handler
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		sink.connection.sendICECandidate(sink.requestId, sink.sid, i.ToJSON().Candidate)
	})

	// Connection status handler
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			sink.connection.logDebug("Sink Disconnected | sinkId: " + fmt.Sprint(sink.sinkId) + " | SreamID: " + sink.sid + " | RequestID: " + sink.requestId)
			sink.reconnect() // If the connection fails, retry it
		} else if state == webrtc.PeerConnectionStateConnected {
			sink.connection.logDebug("Sink Connected | sinkId: " + fmt.Sprint(sink.sinkId) + " | SreamID: " + sink.sid + " | RequestID: " + sink.requestId)
		}
	})

	// Include the audio track
	if sink.hasAudio {
		audioSender, err := peerConnection.AddTrack(sink.localTrackAudio)
		if err != nil {
			LogError(err)
			return
		}

		go readPacketsFromRTPSender(audioSender)
	}

	// Include the video track
	if sink.hasVideo {
		videoSender, err := peerConnection.AddTrack(sink.localTrackVideo)
		if err != nil {
			LogError(err)
			return
		}

		go readPacketsFromRTPSender(videoSender)
	}

	// Generate offer
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		LogError(err)
		return
	}

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		LogError(err)
		return
	}

	// Send OFFER to the client
	sink.connection.sendOffer(sink.requestId, sink.sid, offer.SDP)
}

// Call when an ICE Candidate message is received from the client via websocket
func (sink *WRTC_Sink) onICECandidate(sdp string) {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	if sink.peerConnection == nil {
		return
	}

	err := sink.peerConnection.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: sdp,
	})

	if err != nil {
		LogError(err)
	}
}

// Call when the ANSWER is received from the client via websocket
func (sink *WRTC_Sink) onAnswer(sdp string) {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	if sink.peerConnection == nil {
		return
	}

	// Set the remote SessionDescription
	err := sink.peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})

	if err != nil {
		LogError(err)
	}
}

// Reconnect if the peer connection is closed, but the sink is still active
func (sink *WRTC_Sink) reconnect() {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	sink.peerConnection = nil

	if !sink.closed {
		go sink.runAfterTracksReady()
	}
}

// Close the sink
func (sink *WRTC_Sink) close() {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	sink.closed = true

	if sink.peerConnection != nil {
		sink.peerConnection.OnICECandidate(nil)
		sink.peerConnection.OnConnectionStateChange(nil)
		sink.peerConnection.Close()
	}

	sink.peerConnection = nil
	sink.hasAudio = false
	sink.hasVideo = false
	sink.localTrackAudio = nil
	sink.localTrackVideo = nil
	sink.node.removeSink(sink)
}
