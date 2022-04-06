// Redis service

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
)

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
