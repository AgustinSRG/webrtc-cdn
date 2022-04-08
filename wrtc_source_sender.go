// WebRTC source sender.
// Sends data to other nodes.

package main

import (
	"encoding/json"
	"sync"

	"github.com/pion/webrtc/v3"
)

// WRTC_Source_Sender - This data structure contains the status data
// of an inter-node OUTPUT connection
// Sends the tracks of a WRTC_Source to other node
type WRTC_Source_Sender struct {
	sid      string // WebRTC stream ID
	remoteId string // ID of the remote node

	node *WebRTC_CDN_Node // Reference to the node

	closed bool // True when the source is no longer active

	peerConnection *webrtc.PeerConnection // WebRTC Peer Connection

	statusMutex *sync.Mutex // Mutex to control access to the struct

	hasAudio        bool
	localTrackAudio *webrtc.TrackLocalStaticRTP // Audio track

	hasVideo        bool
	localTrackVideo *webrtc.TrackLocalStaticRTP // Video track
}

// Initialize
func (sender *WRTC_Source_Sender) init() {
	sender.statusMutex = &sync.Mutex{}
	sender.closed = false
}

// Receive the tracks from local source
func (sender *WRTC_Source_Sender) onTracksReady(localTrackVideo *webrtc.TrackLocalStaticRTP, localTrackAudio *webrtc.TrackLocalStaticRTP) {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	// Set video track
	sender.localTrackVideo = localTrackVideo
	sender.hasVideo = localTrackVideo != nil

	// Set audio track
	sender.localTrackAudio = localTrackAudio
	sender.hasAudio = localTrackAudio != nil

	// If there is an existing connection, close it
	if sender.peerConnection != nil {
		sender.peerConnection.OnICECandidate(nil)
		sender.peerConnection.OnConnectionStateChange(nil)
		sender.peerConnection.Close()
	}

	sender.peerConnection = nil

	// Run the connection process
	go sender.runAfterTracksReady()
}

// Starts the peer connection, generates the offer and sets up the event handlers
func (sender *WRTC_Source_Sender) runAfterTracksReady() {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	if !sender.hasVideo && !sender.hasAudio {
		return // Nothing to do
	}

	peerConnectionConfig := loadWebRTCConfig() // Load config

	// Create a new PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		LogError(err)
		return
	}

	sender.peerConnection = peerConnection

	// ICE candidate handler
	peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
		sender.statusMutex.Lock()
		defer sender.statusMutex.Unlock()

		if i != nil {
			b, e := json.Marshal(i.ToJSON())
			if e != nil {
				LogError(e)
			} else {
				sender.sendICECandidate(string(b)) // Send candidate to the remote node
			}
		} else {
			sender.sendICECandidate("") // End of candidates
		}
	})

	// Connection status handler
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			LogDebug("Source Sender Disconnected | RemoteNode: " + sender.remoteId + " | SreamID: " + sender.sid)
			sender.onClose() // If the connection fails, close the sender
		} else if state == webrtc.PeerConnectionStateConnected {
			LogDebug("Source Sender Connected | RemoteNode: " + sender.remoteId + " | SreamID: " + sender.sid)
		}
	})

	// Include the audio track
	if sender.hasAudio {
		audioSender, err := peerConnection.AddTrack(sender.localTrackAudio)
		if err != nil {
			LogError(err)
			return
		}

		go readPacketsFromRTPSender(audioSender)
	}

	// Include the video track
	if sender.hasVideo {
		videoSender, err := peerConnection.AddTrack(sender.localTrackVideo)
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

	// Send OFFER to the remote node
	offerJSON, e := json.Marshal(offer)

	if e != nil {
		LogError(e)
		return
	}

	sender.sendOffer(string(offerJSON))
}

// Called if the peer connection is disconnected
func (sender *WRTC_Source_Sender) onClose() {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	sender.peerConnection = nil

	// Remove the sender from the node
	sender.node.onSenderClosed(sender)
}

// SEND

// Send offer SDP message to the remote node
func (sender *WRTC_Source_Sender) sendOffer(offerJSON string) {
	mp := make(map[string]string)

	mp["type"] = "OFFER"
	mp["src"] = sender.node.id
	mp["dst"] = sender.remoteId
	mp["sid"] = sender.sid
	if sender.hasVideo {
		mp["video"] = "true"
	}
	if sender.hasAudio {
		mp["audio"] = "true"
	}
	mp["data"] = offerJSON

	sender.node.sendRedisMessage(sender.remoteId, &mp)
}

// Send candidate message to the remote node
func (sender *WRTC_Source_Sender) sendICECandidate(candidate string) {
	mp := make(map[string]string)

	mp["type"] = "CANDIDATE"
	mp["src"] = sender.node.id
	mp["dst"] = sender.remoteId
	mp["sid"] = sender.sid
	mp["data"] = candidate

	sender.node.sendRedisMessage(sender.remoteId, &mp)
}

// RECEIVE

// Call when an ICE Candidate message is received from the remote node
func (sender *WRTC_Source_Sender) onICECandidate(candidateJSON string) {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	if sender.peerConnection == nil {
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

	err = sender.peerConnection.AddICECandidate(candidate)

	if err != nil {
		LogError(err)
	}
}

// Call when the ANSWER is received from the remote node
func (sender *WRTC_Source_Sender) onAnswer(answerJSON string) {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	if sender.peerConnection == nil {
		return
	}

	sd := webrtc.SessionDescription{}

	err := json.Unmarshal([]byte(answerJSON), &sd)

	if err != nil {
		LogError(err)
	}

	// Set the remote SessionDescription
	err = sender.peerConnection.SetRemoteDescription(sd)

	if err != nil {
		LogError(err)
	}
}

// Close the sender
func (sender *WRTC_Source_Sender) close() {
	sender.statusMutex.Lock()
	defer sender.statusMutex.Unlock()

	sender.closed = true

	if sender.peerConnection != nil {
		sender.peerConnection.OnICECandidate(nil)
		sender.peerConnection.OnConnectionStateChange(nil)
		sender.peerConnection.Close()
	}

	sender.peerConnection = nil
	sender.hasAudio = false
	sender.hasVideo = false
	sender.localTrackAudio = nil
	sender.localTrackVideo = nil
}
