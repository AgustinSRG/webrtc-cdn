// WebRTC sinks management

package main

// Generates an unique ID for each sink
func (node *WebRTC_CDN_Node) getSinkID() uint64 {
	node.mutexSinkCount.Lock()
	defer node.mutexSinkCount.Unlock()

	node.sinkCount++

	return node.sinkCount
}

// Registers a sink
func (node *WebRTC_CDN_Node) registerSink(sink *WRTC_Sink) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.sinks[sink.sid] == nil {
		node.sinks[sink.sid] = make(map[uint64]*WRTC_Sink)
	}

	node.sinks[sink.sid][sink.sinkId] = sink

	// Is there a ready source for it?
	if node.sources[sink.sid] != nil && node.sources[sink.sid].ready {
		sink.onTracksReady(node.sources[sink.sid].localTrackVideo, node.sources[sink.sid].localTrackAudio)
		return
	}

	// Is there a relay for it?
	if node.relays[sink.sid] != nil && node.relays[sink.sid].ready {
		sink.onTracksReady(node.relays[sink.sid].localTrackVideo, node.relays[sink.sid].localTrackAudio)
		return
	}

	// Can't find any source, maybe other node has it?
	// Announce to other nodes to create the relay
	node.sendResolveMessage(sink.sid)
}

// Removes a sink
func (node *WebRTC_CDN_Node) removeSink(sink *WRTC_Sink) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.sinks[sink.sid] == nil {
		return
	}

	if node.sinks[sink.sid][sink.sinkId] == nil {
		return
	}

	delete(node.sinks[sink.sid], sink.sinkId)

	if len(node.sinks[sink.sid]) == 0 {
		delete(node.sinks, sink.sid)

		// No more sinks for that stream means there is no need for relays
		// Remove it
		if node.relays[sink.sid] != nil {
			node.relays[sink.sid].close()
			delete(node.relays, sink.sid)
		}
	}
}
