// Track piping

package main

import (
	"errors"
	"io"

	"github.com/pion/webrtc/v3"
)

const TRACK_PIPE_BUFFER_LENGTH = 1400

func pipeTrack(remoteTrack *webrtc.TrackRemote, localTrack *webrtc.TrackLocalStaticRTP) {
	rtpBuf := make([]byte, TRACK_PIPE_BUFFER_LENGTH)
	for {
		i, _, readErr := remoteTrack.Read(rtpBuf)
		if readErr != nil {
			return
		}

		// ErrClosedPipe means we don't have any subscribers, this is ok if no peers have connected yet
		if _, err := localTrack.Write(rtpBuf[:i]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			return
		}
	}
}
