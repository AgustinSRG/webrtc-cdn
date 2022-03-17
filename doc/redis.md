# Inter-node communication protocol

Nodes communicate between them using a publish-subscription service (Redis).

All nodes subscribe to the channel `webrtc_cdn`.

Ecah node will automatically generate an identifier and will subscribe to the channel with the same name.

## Message format

Messages are encoded in JSON format.

All messages have a field `type` with the message type.

All messages have a field `src` with the identifier of the node that sent the message.

```json
{
    "type": "TYPE",
    "src": "node-id"
}
```

## Message types

### RESOLVE

This message is sent in order to ask for the location of an specific stream.

The stream ID must be provided in the `sid` property in the message.

```json
{
    "type": "RESOLVE",
    "src": "node-id",
    "sid": "stream-id"
}
```

### INFO

This message is sent to provide information about a stream location.

The stream ID id provided in the `sid` property in the message.

The node ID is provided in the `src` property in the message.

```json
{
    "type": "INFO",
    "src": "node-id",
    "sid": "stream-id"
}
```

### CONNECT

This message is sent in order to open a WebRTC connection between nodes.

The stream ID id provided in the `sid` property in the message.

The destination node ID must be provided in the `dst` property in the message.

```json
{
    "type": "CONNECT",
    "src": "node-id",
    "dst": "node-id",
    "sid": "stream-id"
}
```

When the connect message is received by the node connected to the publisher, it will create a new RTC connection and will send an `OFFER` message back.

### OFFER

This message is sent in order to send an SDP offer (WebRTC protocol).

The stream ID id provided in the `sid` property in the message.

The destination node ID must be provided in the `dst` property in the message.

The sdp message must be provided in the `sdp` property in the message.

```json
{
    "type": "OFFER",
    "src": "node-id",
    "dst": "node-id",
    "sid": "stream-id",
    "sdp": "sdp message"
}
```

### ANSWER

This message is sent in order to send an SDP answer (WebRTC protocol).
The stream ID id provided in the `sid` property in the message.

The destination node ID must be provided in the `dst` property in the message.

The sdp message must be provided in the `sdp` property in the message.

```json
{
    "type": "ANSWER",
    "src": "node-id",
    "dst": "node-id",
    "sid": "stream-id",
    "sdp": "sdp message"
}
```

### CANDIDATE

This message is sent in order to send an ICE candicate (WebRTC protocol).

The stream ID id provided in the `sid` property in the message.

The destination node ID must be provided in the `dst` property in the message.

The sdp message must be provided in the `sdp` property in the message.

```json
{
    "type": "CANDIDATE",
    "src": "node-id",
    "dst": "node-id",
    "sid": "stream-id",
    "sdp": "sdp message"
}
```

## CLOSE

This message is sent to close a WebRTC connection or when the WebRTC connection is closed.

The stream ID id provided in the `sid` property in the message.

The destination node ID must be provided in the `dst` property in the message.

```json
{
    "type": "CLOSE",
    "src": "node-id",
    "dst": "node-id",
    "sid": "stream-id"
}
```