// WebRTC Config

package main

import (
	"os"

	"github.com/pion/webrtc/v3"
)

// This function loads WebRTC config from env variables
func loadWebRTCConfig() webrtc.Configuration {
	peerConnectionConfig := webrtc.Configuration{
		ICEServers: make([]webrtc.ICEServer, 0),
	}

	// STUN server
	stunServer := os.Getenv("STUN_SERVER")
	if stunServer != "" {
		peerConnectionConfig.ICEServers = append(peerConnectionConfig.ICEServers, webrtc.ICEServer{
			URLs: []string{stunServer},
		})
	} else {
		peerConnectionConfig.ICEServers = append(peerConnectionConfig.ICEServers, webrtc.ICEServer{
			URLs: []string{"stun:stun.l.google.com:19302"},
		})
	}

	// TURN server
	turnServer := os.Getenv("TURN_SERVER")
	if turnServer != "" {
		peerConnectionConfig.ICEServers = append(peerConnectionConfig.ICEServers, webrtc.ICEServer{
			URLs:       []string{turnServer},
			Username:   os.Getenv("TURN_USERNAME"),
			Credential: os.Getenv("TURN_PASSWORD"),
		})
	}

	return peerConnectionConfig
}
