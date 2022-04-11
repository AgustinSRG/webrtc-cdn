# Signaling Protocol

Clients connect to the CDN nodes using websocket protocol:

```
ws(s)://{NODE_HOST}:{NODE_PORT}/ws
```

When connected, client and server may exchange signaling messages.

## Message format

The messages are UTF-8 encoded strings, with parts splits by line breaks:
 
  - The first line is the message type
  - After it, the message can have an arbitrary number of arguments. Each argument has a name, followed by a colon and it's value.
  - Optionally, after the arguments, it can be an empty line, followed by the body of the message. In this case, the body will be JSON encoded messages refering to the SDP and candidate exchange.

```
MESSAGE-TYPE
Request-ID: request-id
Auth: auth-token
Argument: value

{body}
...
```

## Message types

Here is the full list of message types, including their purpose and full structure explained.

### Heartbeat

The hearbeat messages are sent each 30 seconds by both, the client and the server.

If the server or the client do not receive a hearbear message during 1 minute, the connection may be closed due to inactivity.

The message does not take any arguments or body.

```
HEARTBEAT
```

### Publish

In order for the client to start publishing, it can use a `PUBLISH` message.

The required arguments are:

 - `Request-ID` - An unique ID for the publish session. Any messages refering to it will have the same identifier as an argument.
 - `Stream-ID` - Unique stream ID. Both the publisher and the players need this identifier to use the same WebRTC stream.
 - `Stream-Type` - Stream type. Can be `AUDIO`, `VIDEO` or `DUAL` depending on the media tracks being published.

Optional arguments:

 - `Auth` - Authorization token. Must be a JSON web token signed with the provided secret in the node configuration and the algorithm `HMAC_256`. The subject must be set to `stream_publish` and a claim with name `sid` is required containing the same value as you provide in `Stream-ID`.

```
PUBLISH
Request-ID: request-id
Stream-ID: stream-id
Stream-Type: DUAL
Auth: auth-token
```

### Play

When a client requests a stream for playing, it can send a `PLAY` message.

The required arguments are:

 - `Request-ID` - An unique ID for the play session. Any messages refering to it will have the same identifier as an argument.
 - `Stream-ID` - Unique stream ID. Both the publisher and the players need this identifier to use the same WebRTC stream.

Optional arguments:

 - `Auth` - Authorization token. Must be a JSON web token signed with the provided secret in the node configuration and the algorithm `HMAC_256`. The subject must be set to `stream_play` and a claim with name `sid` is required containing the same value as you provide in `Stream-ID`.

```
PLAY
Request-ID: request-id
Stream-ID: stream-id
Auth: auth-token
```

### OK

For the `PLAY` and `PUBLISH` messages, when they are successful, the server will respond with an `OK` message.

```
OK
Request-ID: request-id
```

### Error

If an error happens, the server will send an `ERROR` message, with the details of the error in the arguments.

```
ERROR
Error-Code: EXAMPLE_CODE
Error-Message: Example Error message
```

### STANDBY

For the `PLAY` request, if there are no available tracks yet, the client will receive a `STANDBY` message.

It's also sent when a source finishes the transmission, until a new one starts.

```
STANDBY
Request-ID: request-id
```

### Offer

In order to send an SDP offer, an `OFFER` message is required, including the request ID as an argument.

The type and SDP must be encoded as JSON, following the [RTCSessionDescription](https://developer.mozilla.org/en-US/docs/Web/API/RTCSessionDescription) structure.

```
OFFER
Request-ID: request-id

{offer json}
...
```

### Answer

In order to send an SDP answer, an `ANSWER` message is required, including the request ID as an argument.

The type and SDP must be encoded as JSON, following the [RTCSessionDescription](https://developer.mozilla.org/en-US/docs/Web/API/RTCSessionDescription) structure.

```
ANSWER
Request-ID: request-id

{answer json}
...
```

### ICE Candidate

For exchanging ICE candidates, `CANDIDATE` messages are used, including the request ID as an argument.

The candidate information is exchanged in the body in JSON format, following the [RTCIceCandidate](https://developer.mozilla.org/en-US/docs/Web/API/RTCIceCandidate) format.

For the end of candidates message, the body must be empty.

```
CANDIDATE
Request-ID: request-id

{candidate json}
...
```

### Close

When a WebRTC connection is closed, ser verver will send a `CLOSE` message. The client can also send it in order to tell the server to close the connection.

```
CLOSE
Request-ID: request-id
```

Note: When the websocket connection is closed, all associated WebRTC connections will also be closed.

## Publishing sequence

The stream publisshing sequence consists of the following steps

 1. The client sends a `PUBLISH` message
 2. The server responds with an `OK` message, or an `ERROR` message if the publishing request is impossible.
 3. The server will create an offer and send it using an `OFFER` message.
 4. The client will respond using an `ANSWER` message.
 5. Both, client and server will exchange `CANDIDATE` messages.
 6. Once the client wants to stop publishing, it may send a `CLOSE` message or close the websocket connection.

## Playing sequence

 1. The client sends a `PLAY` message.
 2. The server responds with an `OK` message or an `ERROR` message if the play request is impossible.
 3. The server will create an offer and send it using an `OFFER` message. Note: This can take an arbitrary amount of time, since if the stream is not being published, it will wait until it starts being published.
 4. The client will respond using an `ANSWER` message.
 5. Both, client and server will exchange `CANDIDATE` messages.
 6. Once the client wants to stop playing, it may send a `CLOSE` message or close the websocket connection. If the stream ends, the server will close the connection with a `CLOSE` message.

## Error codes

List of error codes for the `ERROR` message, send in the `Error-Code` argument.

| Error code | Description |
|---|---|
| INVALID_AUTH | Invalid authentication provided. |
| INVALID_MESSAGE | Invalid message received. |
| PROTOCOL_ERROR | If the protocol is not followed. For example if two publish messages with the same request ID are received. |
| LIMIT_REQUESTS | The max limit of requests has been reached. In order to make more requests with the same websocket, it is required to close an active request. |
