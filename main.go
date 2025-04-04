// Main

package main

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/joho/godotenv"
)

// Generates an unique ID for the node
func makeId(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

const VERSION = "1.0.0"

// Program entry point
func main() {
	godotenv.Load() // Load env vars

	InitLog()

	LogInfo("Started WebRTC CDN - Version " + VERSION)

	nodeId, err := makeId(20)

	if err != nil {
		LogError(err)
	}

	LogInfo("Assigned node identifier: " + nodeId)

	// Create Node service

	node := WebRTC_CDN_Node{
		id: nodeId,
	}

	// Init node
	node.init()

	// Start redis listener
	go setupRedisListener(&node)

	// Run
	node.run()
}
