// WebRTC CDN Node

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"net/http"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"github.com/netdata/go.d.plugin/pkg/iprange"
)

type WebRTC_CDN_Node struct {
	// Config
	id           string
	redisClient  *redis.Client
	upgrader     *websocket.Upgrader
	reqCount     uint64
	sinkCount    uint64
	ipLimit      uint32
	requestLimit uint32

	// Sync
	mutexReqCount  *sync.Mutex
	mutexSinkCount *sync.Mutex
	mutexIpCount   *sync.Mutex
	mutexRedisSend *sync.Mutex
	mutexStatus    *sync.Mutex

	// Status
	connections map[uint64]*Connection_Handler
	ipCount     map[string]uint32

	sources map[string]*WRTC_Source
	relays  map[string]*WRTC_Relay

	sinks   map[string]map[uint64]*WRTC_Sink
	senders map[string]map[string]*WRTC_Source_Sender
}

func (node *WebRTC_CDN_Node) init() {
	// Mutex
	node.mutexReqCount = &sync.Mutex{}
	node.mutexIpCount = &sync.Mutex{}
	node.mutexRedisSend = &sync.Mutex{}
	node.mutexStatus = &sync.Mutex{}
	node.mutexSinkCount = &sync.Mutex{}

	// Status
	node.connections = make(map[uint64]*Connection_Handler)
	node.ipCount = make(map[string]uint32)
	node.sources = make(map[string]*WRTC_Source)
	node.relays = make(map[string]*WRTC_Relay)
	node.sinks = make(map[string]map[uint64]*WRTC_Sink)
	node.senders = make(map[string]map[string]*WRTC_Source_Sender)

	// Config
	node.ipLimit = 4
	custom_ip_limit := os.Getenv("MAX_IP_CONCURRENT_CONNECTIONS")
	if custom_ip_limit != "" {
		cil, e := strconv.Atoi(custom_ip_limit)
		if e != nil {
			node.ipLimit = uint32(cil)
		}
	}

	node.reqCount = 0
	node.sinkCount = 0

	node.requestLimit = 100
	custom_req_limit := os.Getenv("MAX_REQUESTS_PER_SOCKET")
	if custom_req_limit != "" {
		cil, e := strconv.Atoi(custom_req_limit)
		if e != nil {
			node.requestLimit = uint32(cil)
		}
	}
}

// Request IDs

func (node *WebRTC_CDN_Node) getRequestID() uint64 {
	node.mutexReqCount.Lock()
	defer node.mutexReqCount.Unlock()

	node.reqCount++

	return node.reqCount
}

// SINK IDs

func (node *WebRTC_CDN_Node) getSinkID() uint64 {
	node.mutexSinkCount.Lock()
	defer node.mutexSinkCount.Unlock()

	node.sinkCount++

	return node.sinkCount
}

// IP LIMIT

func (node *WebRTC_CDN_Node) AddIP(ip string) bool {
	node.mutexIpCount.Lock()
	defer node.mutexIpCount.Unlock()

	c := node.ipCount[ip]

	if c >= node.ipLimit {
		return false
	}

	node.ipCount[ip] = c + 1

	return true
}

func (node *WebRTC_CDN_Node) isIPExempted(ipStr string) bool {
	r := os.Getenv("CONCURRENT_LIMIT_WHITELIST")

	if r == "" {
		return false
	}

	if r == "*" {
		return true
	}

	ip := net.ParseIP(ipStr)

	parts := strings.Split(r, ",")

	for i := 0; i < len(parts); i++ {
		rang, e := iprange.ParseRange(parts[i])

		if e != nil {
			LogError(e)
			continue
		}

		if rang.Contains(ip) {
			return true
		}
	}

	return false
}

func (node *WebRTC_CDN_Node) RemoveIP(ip string) {
	node.mutexIpCount.Lock()
	defer node.mutexIpCount.Unlock()

	c := node.ipCount[ip]

	if c <= 1 {
		delete(node.ipCount, ip)
	} else {
		node.ipCount[ip] = c - 1
	}
}

// REDIS (inter-node)

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

func (node *WebRTC_CDN_Node) sendInfoMessage(channel string, sid string) {
	mp := make(map[string]string)

	mp["type"] = "INFO"
	mp["src"] = node.id
	mp["sid"] = sid

	node.sendRedisMessage(channel, &mp)
}

func (node *WebRTC_CDN_Node) sendResolveMessage(sid string) {
	mp := make(map[string]string)

	mp["type"] = "RESOLVE"
	mp["src"] = node.id
	mp["sid"] = sid

	node.sendRedisMessage(REDIS_BROADCAST_CHANNEL, &mp)
}

func (node *WebRTC_CDN_Node) sendConnectMessage(dst string, sid string) {
	mp := make(map[string]string)

	mp["type"] = "CONNECT"
	mp["src"] = node.id
	mp["dst"] = dst
	mp["sid"] = sid

	node.sendRedisMessage(dst, &mp)
}

// HTTP / HTTPS Servers (SIGNALING)

func (node *WebRTC_CDN_Node) runHTTPSecureServer(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup HTTPS server
	var ssl_port int
	ssl_port = 443
	customSSLPort := os.Getenv("SSL_PORT")
	if customSSLPort != "" {
		sslp, e := strconv.Atoi(customSSLPort)
		if e == nil {
			ssl_port = sslp
		}
	}

	certFile := os.Getenv("SSL_CERT")
	keyFile := os.Getenv("SSL_KEY")

	if certFile != "" && keyFile != "" {
		LogInfo("[SSL] Listening on " + bind_addr + ":" + strconv.Itoa(ssl_port))
		errSSL := http.ListenAndServeTLS(bind_addr+":"+strconv.Itoa(ssl_port), certFile, keyFile, node)

		if errSSL != nil {
			LogError(errSSL)
		}
	}
}

func (node *WebRTC_CDN_Node) runHTTPServer(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup RTMP server
	var tcp_port int
	tcp_port = 80
	customTCPPort := os.Getenv("HTTP_PORT")
	if customTCPPort != "" {
		tcpp, e := strconv.Atoi(customTCPPort)
		if e == nil {
			tcp_port = tcpp
		}
	}

	LogInfo("[HTTP] Listening on " + bind_addr + ":" + strconv.Itoa(tcp_port))
	errHTTP := http.ListenAndServe(bind_addr+":"+strconv.Itoa(tcp_port), node)

	if errHTTP != nil {
		LogError(errHTTP)
	}
}

func (node *WebRTC_CDN_Node) run() {
	// Setup Redis sender

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

	if redisTLS == "YES" {
		node.redisClient = redis.NewClient(&redis.Options{
			Addr:      redisHost + ":" + redisPort,
			Password:  redisPassword,
			TLSConfig: &tls.Config{},
		})
	} else {
		node.redisClient = redis.NewClient(&redis.Options{
			Addr:     redisHost + ":" + redisPort,
			Password: redisPassword,
		})
	}

	// Setup websocket handler

	node.upgrader = &websocket.Upgrader{}
	node.upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	var wg sync.WaitGroup

	wg.Add(2)

	go node.runHTTPServer(&wg)
	go node.runHTTPSecureServer(&wg)

	wg.Wait()
}

func (node *WebRTC_CDN_Node) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqId := node.getRequestID()

	LogRequest(reqId, req.RemoteAddr, ""+req.Method+" "+req.RequestURI)

	if req.URL.Path == "/ws" {
		if !node.isIPExempted(req.RemoteAddr) {
			if !node.AddIP(req.RemoteAddr) {
				w.WriteHeader(429)
				fmt.Fprintf(w, "Too many requests.")
				LogRequest(reqId, req.RemoteAddr, "Connection rejected: Too many requests")
				return
			}
		}

		c, err := node.upgrader.Upgrade(w, req, nil)
		if err != nil {
			LogError(err)
			return
		}

		handler := Connection_Handler{
			id:         reqId,
			ip:         req.RemoteAddr,
			node:       node,
			connection: c,
		}

		handler.init()

		node.mutexStatus.Lock()

		node.connections[reqId] = &handler

		node.mutexStatus.Unlock()

		go handler.run()
	} else if req.URL.Path == "/test" {
		w.Header().Add("Content-Type", "text/html")
		w.WriteHeader(200)
		fmt.Fprint(w, TEST_CLIENT_HTML)
	} else {
		w.WriteHeader(200)
		fmt.Fprintf(w, "WebRTC-CDN Signaling Server. Connect to /ws for signaling")
	}
}

// CLOSE

func (node *WebRTC_CDN_Node) onClose(id uint64) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	delete(node.connections, id)
}

// SOURCES

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
}

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

func (node *WebRTC_CDN_Node) onSourceClosed(source *WRTC_Source) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	delete(node.sources, source.sid)

	// Remove all the senders
	if node.senders[source.sid] != nil {
		for _, sender := range node.senders[source.sid] {
			sender.close()
		}
	}

	delete(node.senders, source.sid)
}

func (node *WebRTC_CDN_Node) resolveSource(sid string) bool {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	return node.sources[sid] != nil
}

// SINKS

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

// SOURCE SENDERS

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

func (node *WebRTC_CDN_Node) receiveAnswerMessage(from string, sid string, sdp string) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.senders[sid] != nil && node.senders[sid][from] != nil {
		node.senders[sid][from].onAnswer(sdp)
	}
}

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

// RELAYS

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

func (node *WebRTC_CDN_Node) receiveOfferMessage(from string, sid string, data string, hasVideo bool, hasAudio bool) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	if node.relays[sid] != nil {
		go node.relays[sid].onOffer(data, hasVideo, hasAudio)
	}
}

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

func (node *WebRTC_CDN_Node) onRelayClosed(relay *WRTC_Relay) {
	node.mutexStatus.Lock()
	defer node.mutexStatus.Unlock()

	delete(node.relays, relay.sid)

	// If there are sinks for that stream ID
	// and there are no source, try resolving it
	if node.sinks[relay.sid] != nil && len(node.sinks[relay.sid]) > 0 {
		node.sendResolveMessage(relay.sid)
	}
}
