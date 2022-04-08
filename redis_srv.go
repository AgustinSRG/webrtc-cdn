// Redis service

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// This channel is used to boardacas messages to all the nodes
const REDIS_BROADCAST_CHANNEL = "webrtc_cdn"

// Setup redis client to receive messages
func setupRedisListener(node *WebRTC_CDN_Node) {
	defer func() {
		if err := recover(); err != nil {
			switch x := err.(type) {
			case string:
				LogError(errors.New(x))
			case error:
				LogError(x)
			default:
				LogError(errors.New("Could not connect to Redis"))
			}
		}
		LogWarning("Connection to Redis lost!")
	}()

	// Load configuration

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}

	redisPort := os.Getenv("REDIS_PORT")
	if redisPort == "" {
		redisPort = "6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	redisTLS := os.Getenv("REDIS_TLS")

	ctx := context.Background()

	// Connect

	var redisClient *redis.Client

	if redisTLS == "YES" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:      redisHost + ":" + redisPort,
			Password:  redisPassword,
			TLSConfig: &tls.Config{},
		})
	} else {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     redisHost + ":" + redisPort,
			Password: redisPassword,
		})
	}

	// Subscribe to the channels

	subscriber := redisClient.Subscribe(ctx, REDIS_BROADCAST_CHANNEL, node.id)

	LogInfo("[REDIS] Listening for commands on channels '" + REDIS_BROADCAST_CHANNEL + "', '" + node.id + "'")

	for {
		msg, err := subscriber.ReceiveMessage(ctx) // Receive message

		if err != nil {
			LogWarning("Could not connect to Redis: " + err.Error())
			time.Sleep(10 * time.Second)
		} else {
			// Parse message
			node.receiveRedisMessage(msg.Payload)
		}
	}
}

// Parses messages received from redis
// and calls the corresponding functions
func (node *WebRTC_CDN_Node) receiveRedisMessage(msg string) {
	msgData := map[string]string{}

	// Decode message
	json.Unmarshal([]byte(msg), &msgData)

	msgType := strings.ToUpper(msgData["type"])
	msgSource := msgData["src"]

	if msgSource == node.id {
		return // Ignore messages from self
	}

	switch msgType {
	case "RESOLVE":
		sid := msgData["sid"]
		if node.resolveSource(sid) {
			node.sendInfoMessage(msgSource, sid) // Tell the node who asked that we have that source
		}
	case "INFO":
		sid := msgData["sid"]
		node.receiveInfoMessage(msgSource, sid)
	case "CONNECT":
		sid := msgData["sid"]
		node.receiveConnectMessage(msgSource, sid)
	case "OFFER":
		sid := msgData["sid"]
		data := msgData["data"]
		hasVideo := (msgData["video"] == "true")
		hasAudio := (msgData["audio"] == "true")
		node.receiveOfferMessage(msgSource, sid, data, hasVideo, hasAudio)
	case "ANSWER":
		sid := msgData["sid"]
		data := msgData["data"]
		node.receiveAnswerMessage(msgSource, sid, data)
	case "CANDIDATE":
		sid := msgData["sid"]
		data := msgData["data"]
		node.receiveCandidateMessage(msgSource, sid, data)
	}
}

// Sends a redis message
func (node *WebRTC_CDN_Node) sendRedisMessage(channel string, msg *map[string]string) {
	b, e := json.Marshal(msg)
	if e != nil {
		LogError(e)
		return
	}

	node.mutexRedisSend.Lock()
	defer node.mutexRedisSend.Unlock()

	r := node.redisClient.Publish(context.Background(), channel, string(b))
	if r != nil && r.Err() != nil {
		LogError(r.Err())
	} else {
		LogDebug("[REDIS] [SENT] Channel: " + channel + " | Message: " + string(b))
	}
}

// Sends an INFO message to othe node(s)
// This message makes them aware the node has a WebRTC source
// for the specified Stream ID (sid)
func (node *WebRTC_CDN_Node) sendInfoMessage(channel string, sid string) {
	mp := make(map[string]string)

	mp["type"] = "INFO"
	mp["src"] = node.id
	mp["sid"] = sid

	node.sendRedisMessage(channel, &mp)
}

// Sends a RESOLVE message
// This message asks other nodes if they have a WebRTC source
// for the specified Stream ID (sid)
// They will respond with INFO if they have it
func (node *WebRTC_CDN_Node) sendResolveMessage(sid string) {
	mp := make(map[string]string)

	mp["type"] = "RESOLVE"
	mp["src"] = node.id
	mp["sid"] = sid

	node.sendRedisMessage(REDIS_BROADCAST_CHANNEL, &mp)
}

// Sends a CONNECT message
// This message asks a node to open a connection
// to receive an external WebRTC source
func (node *WebRTC_CDN_Node) sendConnectMessage(dst string, sid string) {
	mp := make(map[string]string)

	mp["type"] = "CONNECT"
	mp["src"] = node.id
	mp["dst"] = dst
	mp["sid"] = sid

	node.sendRedisMessage(dst, &mp)
}
