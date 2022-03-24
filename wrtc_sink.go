// WebRTC sink connection.
// Sends data to a local websocket connection.

package main

import (
	"sync"

	"github.com/pion/webrtc/v3"
)

type WRTC_Sink struct {
	requestId string
	sid       string
	node      *WebRTC_CDN_Node

	closed bool

	peerConnection *webrtc.PeerConnection
	statusMutex    *sync.Mutex

	connection *Connection_Handler

	hasAudio bool
	hasVideo bool

	localTrackVideo *webrtc.TrackLocalStaticRTP
	localTrackAudio *webrtc.TrackLocalStaticRTP
}

func (sink *WRTC_Sink) init() {
	sink.closed = false
	sink.statusMutex = &sync.Mutex{}
}

// Receive the tracks from local source or relay
func (sink *WRTC_Sink) onTracksReady(localTrackVideo *webrtc.TrackLocalStaticRTP, localTrackAudio *webrtc.TrackLocalStaticRTP) {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	sink.localTrackVideo = localTrackVideo
	sink.localTrackAudio = localTrackAudio

	sink.hasAudio = localTrackAudio != nil
	sink.hasVideo = localTrackVideo != nil

	go sink.runAfterTracksReady()
}

// Start the peer connection
func (sink *WRTC_Sink) runAfterTracksReady() {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	peerConnectionConfig := loadWebRTCConfig()

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		sink.onClose()
		return
	}

	sink.peerConnection = peerConnection

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		sink.connection.sendICECandidate(sink.requestId, sink.sid, i.ToJSON().Candidate)
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			sink.onClose()
		}
	})

	if sink.hasAudio {
		audioSender, err := peerConnection.AddTrack(sink.localTrackAudio)
		if err != nil {
			LogError(err)
			sink.onClose()
			return
		}

		go readPacketsFromRTPSender(audioSender)
	}

	if sink.hasVideo {
		videoSender, err := peerConnection.AddTrack(sink.localTrackVideo)
		if err != nil {
			LogError(err)
			sink.onClose()
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

	// Send to the connection
	sink.connection.sendOffer(sink.requestId, sink.sid, offer.SDP)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		LogError(err)
		return
	}
}

func (sink *WRTC_Sink) onICECandidate(sdp string) {
	err := sink.peerConnection.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: sdp,
	})
	if err != nil {
		LogError(err)
	}
}

func (sink *WRTC_Sink) onAnswer(sdp string) {
	// Set the remote SessionDescription
	err := sink.peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	})
	if err != nil {
		LogError(err)
		return
	}
}

func (sink *WRTC_Sink) onClose() {
	sink.statusMutex.Lock()
	defer sink.statusMutex.Unlock()

	if sink.closed {
		return
	}
	sink.closed = true

	// Remove all the linked connections
}

func (sink *WRTC_Sink) close() {
}
