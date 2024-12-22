// Controls the logic of WebRTC senders and relays
// Functionality of inter-node interaction

package main

// Called when a CONNECT message is received
// If the node has a WebRTC source for the specified Stream ID,
// it will create a sender for the node that requested the connection
func (node *WebRTC_CDN_Node) receiveConnectMessage(from string, sid string) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	source := node.sources[sid]

	if source == nil {
		return // Ignore, no source available
	}

	if node.senders[sid] != nil && node.senders[sid][from] != nil {
		// Close previous sender
		node.senders[sid][from].close()
	}

	// Create a sender
	sender := WRTC_Source_Sender{
		sid:      sid,
		remoteId: from,
		node:     node,
	}

	sender.init()

	if node.senders[sid] == nil {
		node.senders[sid] = make(map[string]*WRTC_Source_Sender)
	}

	node.senders[sid][from] = &sender

	if node.sources[sid] != nil && node.sources[sid].ready {
		// Tracks already available
		sender.onTracksReady(node.sources[sid].localTrackVideo, node.sources[sid].localTrackAudio)
	}
}

// Called when a sender is closed (disconnection)
func (node *WebRTC_CDN_Node) onSenderClosed(sender *WRTC_Source_Sender) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.senders[sender.sid] != nil && node.senders[sender.sid][sender.remoteId] != nil {
		delete(node.senders[sender.sid], sender.remoteId)

		if len(node.senders[sender.sid]) == 0 {
			delete(node.senders, sender.sid)
		}
	}
}

// Called when an ANSWER message is received
// This message is managed by the sender
func (node *WebRTC_CDN_Node) receiveAnswerMessage(from string, sid string, sdp string) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.senders[sid] != nil && node.senders[sid][from] != nil {
		node.senders[sid][from].onAnswer(sdp)
	}
}

// Called when a CANDIDATE message is received
// It may be forwarded to a sender or a relay
func (node *WebRTC_CDN_Node) receiveCandidateMessage(from string, sid string, candidate string) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	// Check senders
	if node.senders[sid] != nil && node.senders[sid][from] != nil {
		node.senders[sid][from].onICECandidate(candidate)
	}

	// Check relays
	if node.relays[sid] != nil {
		node.relays[sid].onICECandidate(candidate)
	}
}

// Called when an INFO message is received
func (node *WebRTC_CDN_Node) receiveInfoMessage(from string, sid string) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	// If we receive an INFO message from another node
	// and we have an existing connection for that stream,
	// we must close it to prevent duplicates
	if node.sources[sid] != nil {
		// Close the old source
		s := node.sources[sid]
		s.close(true, false)
		delete(node.sources, sid)
	}

	// If we have any pending sinks for that stream,
	// we create a relay for that source
	if node.sinks[sid] != nil && len(node.sinks[sid]) > 0 {
		// Close old relay
		if node.relays[sid] != nil {
			node.relays[sid].close()
			delete(node.relays, sid)
		}

		// Create new relay
		relay := WRTC_Relay{
			sid:      sid,
			remoteId: from,
			node:     node,
		}

		relay.init()

		node.relays[sid] = &relay

		// Send a connect message
		node.sendConnectMessage(from, sid)
	}
}

// Called when an OFFER message is received
// This message is managed by the relay
func (node *WebRTC_CDN_Node) receiveOfferMessage(sid string, data string, hasVideo bool, hasAudio bool) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.relays[sid] != nil {
		go node.relays[sid].onOffer(data, hasVideo, hasAudio)
	}
}

// Called when a relay is ready
// This means the tracks are available
func (node *WebRTC_CDN_Node) onRelayReady(relay *WRTC_Relay) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	relay.ready = true

	// Notify sinks
	if node.sinks[relay.sid] != nil {
		for _, sink := range node.sinks[relay.sid] {
			sink.onTracksReady(relay.localTrackVideo, relay.localTrackAudio)
		}
	}
}

// Called when a relay is closed
func (node *WebRTC_CDN_Node) onRelayClosed(relay *WRTC_Relay) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	delete(node.relays, relay.sid)

	// Any sinks waiting, tell them the tracks are closed
	if node.sinks[relay.sid] != nil {
		for _, sink := range node.sinks[relay.sid] {
			sink.onTracksClosed(relay.localTrackVideo, relay.localTrackAudio)
		}
	}

	// If there are sinks for that stream ID
	// and there are no source, try resolving it
	if node.sinks[relay.sid] != nil && len(node.sinks[relay.sid]) > 0 {
		node.sendResolveMessage(relay.sid)
	}
}
