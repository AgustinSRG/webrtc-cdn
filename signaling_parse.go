// Signaling messages parsing

package main

import "strings"

type SignalingMessage struct {
	method string
	params map[string]string
	body   string
}

func parseSignalingMessage(raw string) SignalingMessage {
	lines := strings.Split(raw, "\n")
	msg := SignalingMessage{
		method: "",
		params: make(map[string]string),
		body:   "",
	}

	if len(lines) > 0 {
		msg.method = strings.ToUpper(strings.Trim(lines[0], " \n\r\t"))
	}

	var isBody bool = false

	for i := 1; i < len(lines); i++ {
		line := lines[i]

		if line == "" {
			// Found empty line
			isBody = true
			continue
		}

		if isBody {
			// Body
			if msg.body == "" {
				msg.body = line
			} else {
				msg.body += "\n" + line
			}
		} else {
			// Param
			colonIndex := strings.Index(line, ":")
			if colonIndex > 0 {
				key := strings.ToLower(strings.Trim(line[0:colonIndex], " \n\r\t"))
				val := strings.Trim(line[colonIndex+1:], " \n\r\t")
				msg.params[key] = val
			}
		}
	}

	return msg
}

func (s SignalingMessage) serialize() string {
	var raw string
	raw = strings.ToUpper(s.method) + "\n"

	if s.params != nil {
		for key, val := range s.params {
			raw += key + ":" + val + "\n"
		}
	}

	if s.body != "" {
		raw += "\n" + s.body
	}

	return raw
}
