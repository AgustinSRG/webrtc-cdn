package main

import (
	"crypto/rand"
	"encoding/hex"
)

func makeId(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func main() {
	LogInfo("WebRTC CDN (Version 1.0.0)")

	nodeId, err := makeId(20)

	if err != nil {
		LogError(err)
	}

	LogInfo("Assigned node identifier: " + nodeId)

	// Create Node service

	node := WebRTC_CDN_Node{
		id: nodeId,
	}

	// Start redis listener
	go setupRedisListener(&node)

	// Run
	node.run()

	<-(chan int)(nil) // Block forever
}
