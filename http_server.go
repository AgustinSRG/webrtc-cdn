// HTTP Server

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Generates unique ID for each request
func (node *WebRTC_CDN_Node) getRequestID() uint64 {
	node.mutexReqCount.Lock()
	defer node.mutexReqCount.Unlock()

	node.reqCount++

	return node.reqCount
}

// Adds IP address to the list
// Returns false if the limit has been reached
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

// Checks if an IP is exempted from the limit
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
		_, rang, e := net.ParseCIDR(parts[i])

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

// Removes an IP from the list
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

// Runs secure HTTPs server
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
		// Listen
		LogInfo("[SSL] Listening on " + bind_addr + ":" + strconv.Itoa(ssl_port))
		errSSL := http.ListenAndServeTLS(bind_addr+":"+strconv.Itoa(ssl_port), certFile, keyFile, node)

		if errSSL != nil {
			LogError(errSSL)
		}
	}
}

// Runs HTTP server
func (node *WebRTC_CDN_Node) runHTTPServer(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()

	bind_addr := os.Getenv("BIND_ADDRESS")

	// Setup HTTP server
	var tcp_port int
	tcp_port = 80
	customTCPPort := os.Getenv("HTTP_PORT")
	if customTCPPort != "" {
		tcpp, e := strconv.Atoi(customTCPPort)
		if e == nil {
			tcp_port = tcpp
		}
	}

	// Listen
	LogInfo("[HTTP] Listening on " + bind_addr + ":" + strconv.Itoa(tcp_port))
	errHTTP := http.ListenAndServe(bind_addr+":"+strconv.Itoa(tcp_port), node)

	if errHTTP != nil {
		LogError(errHTTP)
	}
}

// Handles HTTP/HTTPS requests from clients
func (node *WebRTC_CDN_Node) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqId := node.getRequestID()

	ip, _, err := net.SplitHostPort(req.RemoteAddr)

	if err != nil {
		LogError(err)
		w.WriteHeader(200)
		fmt.Fprintf(w, "WebRTC-CDN Signaling Server. Connect to /ws for signaling\nGo to /test for testing.")
		return
	}

	LogRequest(reqId, ip, ""+req.Method+" "+req.RequestURI)

	if req.URL.Path == "/ws" {
		// Websocket signaling connection
		if !node.isIPExempted(ip) {
			if !node.AddIP(ip) {
				w.WriteHeader(429)
				fmt.Fprintf(w, "Too many requests.")
				LogRequest(reqId, ip, "Connection rejected: Too many requests")
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
			ip:         ip,
			node:       node,
			connection: c,
		}

		handler.init()

		node.mutexConnections.Lock()

		node.connections[reqId] = &handler

		node.mutexConnections.Unlock()

		go handler.run()
	} else {
		w.WriteHeader(200)
		fmt.Fprintf(w, "WebRTC-CDN Signaling Server. Connect to /ws for signaling")
	}
}

// Called after a connection is closed
func (node *WebRTC_CDN_Node) onConnectionClose(id uint64, ip string) {
	node.mutexConnections.Lock()
	defer node.mutexConnections.Unlock()

	node.RemoveIP(ip) // Remove ip count

	delete(node.connections, id)
}
