// WebRTC source connection.
// Receives data from a local websocket connection.
// Can send data to other local connections or to other nodes

package main

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v3"
)

const rtcpPLIInterval = time.Second * 2

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
func (source *WRTC_Source) run() {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	peerConnectionConfig := loadWebRTCConfig() // Load config

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		return
	}

	source.peerConnection = peerConnection

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
				return
			}

			source.localTrackVideo = localTrack

			go pipeTrack(remoteTrack, localTrack)

			// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
			// This can be less wasteful by processing incoming RTCP events, then we would emit a NACK/PLI when a viewer requests it
			go func() {
				ticker := time.NewTicker(rtcpPLIInterval)
				for range ticker.C {
					if rtcpSendErr := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remoteTrack.SSRC())}}); rtcpSendErr != nil {
						if errors.Is(rtcpSendErr, io.ErrClosedPipe) {
							return
						}
					}
				}
			}()
		} else if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			// Received audio track
			if source.localTrackAudio != nil {
				return
			}

			localTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, "audio", "pion")
			if newTrackErr != nil {
				LogError(newTrackErr)
				return
			}

			source.localTrackAudio = localTrack

			go pipeTrack(remoteTrack, localTrack)
		} else {
			return
		}

		if (!source.hasAudio || source.localTrackAudio != nil) && (!source.hasVideo || source.localTrackVideo != nil) {
			// Received all the tracks
			source.connection.logDebug("Source Ready | SreamID: " + source.sid + " | RequestID: " + source.requestId)
			source.node.onSourceReady(source)
		}
	})

	// ICE Candidate handler
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		source.statusMutex.Lock()
		defer source.statusMutex.Unlock()

		if i != nil {
			b, e := json.Marshal(i.ToJSON())
			if e != nil {
				LogError(e)
			} else {
				source.connection.sendICECandidate(source.requestId, source.sid, string(b))
			}
		} else {
			source.connection.sendICECandidate(source.requestId, source.sid, "") // End of candidates
		}
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
			return
		}
	}

	if source.hasAudio {
		// Create transceiver to receive an AUDIO track
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

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(offer)
	if err != nil {
		LogError(err)
		return
	}

	// Send to the client

	offerJSON, e := json.Marshal(offer)

	if e != nil {
		LogError(e)
		return
	}

	source.connection.sendOffer(source.requestId, source.sid, string(offerJSON))
}

// ICE Candidate message received from the client
func (source *WRTC_Source) onICECandidate(candidateJSON string) {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.peerConnection == nil {
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

	err = source.peerConnection.AddICECandidate(candidate)

	if err != nil {
		LogError(err)
	}
}

// ANSWER message received from the client
func (source *WRTC_Source) onAnswer(answerJSON string) {
	source.statusMutex.Lock()
	defer source.statusMutex.Unlock()

	if source.peerConnection == nil {
		return
	}

	sd := webrtc.SessionDescription{}

	err := json.Unmarshal([]byte(answerJSON), &sd)

	if err != nil {
		LogError(err)
	}

	// Set the remote SessionDescription
	err = source.peerConnection.SetRemoteDescription(sd)

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
