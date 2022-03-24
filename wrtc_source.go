// WebRTC source connection.
// Receives data from a local websocket connection.
// Can send data to other local connections or to other nodes

package main

import (
	"sync"

	"github.com/pion/webrtc/v3"
)

type WRTC_Source struct {
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

func (source *WRTC_Source) init() {
	source.closed = false
	source.statusMutex = &sync.Mutex{}
}

func (source *WRTC_Source) run() {
	peerConnectionConfig := loadWebRTCConfig()

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		source.onClose()
		return
	}

	source.peerConnection = peerConnection

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		source.statusMutex.Lock()
		defer source.statusMutex.Unlock()

		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			if source.localTrackVideo != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "video", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
			}

			source.localTrackVideo = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			if source.localTrackAudio != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "audio", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
			}

			source.localTrackAudio = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else {
			return
		}

		if (!source.hasAudio || source.localTrackAudio != nil) && (!source.hasVideo || source.localTrackVideo != nil) {
			// Received all the tracks

		}
	})

	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		source.connection.sendICECandidate(source.requestId, source.sid, i.ToJSON().Candidate)
	})

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			source.onClose()
		}
	})

	// Create transcievers
	if source.hasVideo {
		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
			LogError(err)
			return
		}
	}

	if source.hasAudio {
		if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
			LogError(err)
			return
		}
	}

	// Generate offer
	offer, err := peerConnection.CreateOffer(nil)
	if err != nil {
		LogError(err)
		return
	}

	// Send to the connection
	source.connection.sendOffer(source.requestId, source.sid, offer.SDP)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		LogError(err)
		return
	}
}

func (source *WRTC_Source) onICECandidate(sdp string) {
	err := source.peerConnection.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: sdp,
	})
	if err != nil {
		LogError(err)
	}
}

func (source *WRTC_Source) onAnswer(sdp string) {
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

func (source *WRTC_Source) onClose() {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.closed {
		return
	}
	source.closed = true

	// Remove all the linked connections
}