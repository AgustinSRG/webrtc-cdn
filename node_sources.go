// WebRTC sources management

package main

// Checks if the node has a WebRTC source
// for the specified Stream Id (sid)
func (node *WebRTC_CDN_Node) resolveSource(sid string) bool {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	return node.sources[sid] != nil
}

// Registers a WebRTC source
func (node *WebRTC_CDN_Node) registerSource(source *WRTC_Source) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.sources[source.sid] != nil {
		// Close the old source
		s := node.sources[source.sid]
		s.close(true, false)
	}

	node.sources[source.sid] = source

	// Remove any relays for that source
	if node.relays[source.sid] != nil {
		node.relays[source.sid].close()
		delete(node.relays, source.sid)
	}

	// Remove all the senders
	if node.senders[source.sid] != nil {
		for _, sender := range node.senders[source.sid] {
			sender.close()
		}
		delete(node.senders, source.sid)
	}

	// Announce to other nodes
	node.sendInfoMessage(REDIS_BROADCAST_CHANNEL, source.sid)
}

// Called when a WebRTC source is ready
// This means the tracks can be played
func (node *WebRTC_CDN_Node) onSourceReady(source *WRTC_Source) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	source.ready = true

	// Notify sinks
	if node.sinks[source.sid] != nil {
		for _, sink := range node.sinks[source.sid] {
			sink.onTracksReady(source.localTrackVideo, source.localTrackAudio)
		}
	}

	// Notify senders
	if node.senders[source.sid] != nil {
		for _, sender := range node.senders[source.sid] {
			sender.onTracksReady(source.localTrackVideo, source.localTrackAudio)
		}
	}
}

// Called when the WebRTC source closes by itself (by the client, a disconnection, etc)
// Do not call from another status methods
func (node *WebRTC_CDN_Node) onSourceClosed(source *WRTC_Source) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	delete(node.sources, source.sid)

	// Close and remove all the senders
	if node.senders[source.sid] != nil {
		for _, sender := range node.senders[source.sid] {
			sender.close()
		}

		delete(node.senders, source.sid)
	}

	// Any sinks waiting, tell them the tracks are closed
	if node.sinks[source.sid] != nil {
		for _, sink := range node.sinks[source.sid] {
			sink.onTracksClosed(source.localTrackVideo, source.localTrackAudio)
		}
	}
}
