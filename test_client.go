// Test client

package main

const TEST_CLIENT_HTML = `
<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta http-equiv="X-UA-Compatible" content="IE=edge">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>WebRTC CDN test client</title>
    <style>
        *,
        ::after,
        ::before {
            box-sizing: border-box;
        }

        body {
            margin: 0;
            padding: 0.5rem;
            width: 100%;
        }

        .form-group {
            padding-bottom: 0.75rem;
        }

        .text-input {
            display: block;
            width: 100%;
            padding: 0.25rem;
            margin: 0;
        }

        label {
            display: block;
            font-weight: bold;
            padding-bottom: 0.25rem;
        }

        #test_video {
            background-color: black;
        }

        button {
            padding: 0.5rem 1rem;
            font-size: large;
            font-weight: bold;
            text-transform: uppercase;
        }

        .error-msg {
            color: red;
        }
    </style>
</head>

<body>
    <h2>WebRTC CDN test client</h2>
    <div class="test-form">
        <div class="form-group">
            <label>Stream ID:</label>
            <input id="test_stream_id" type="text" autocomplete="off" maxlength="255" class="text-input" value="test">
        </div>
        <div class="form-group">
            <label>Authorization token:</label>
            <input id="test_auth" type="text" autocomplete="off" class="text-input">
        </div>
        <div class="form-group">
            <label>RTCPeerConnection configuration:</label>
            <textarea id="test_config" class="text-input">
{
    "iceServers": [
        { "urls": "stun:stun.l.google.com:19302" }
    ],
    "sdpSemantics": "unified-plan"
}
            </textarea>
        </div>
        <div class="form-group">
            <div id="test_error" class="error-msg"></div>
            <button id="publish_button" type="button">Publish (Camera)</button>
            <button id="publish_screen_button" type="button">Publish (Screen)</button>
            <button id="watch_button" type="button">Watch</button>
            <button id="stop_button" type="button" disabled>Stop</button>
        </div>
    </div>
    <div class="video-container">
        <div id="test_status" class="form-group">Status: Disconnected</div>
        <video id="test_video"></video>
    </div>
    <script>
        var streamID = "";
        var authToken = "";
        var rtcConfig = {};

        var ws = null;
        var peerConnection = null;
        var mediaStream = null;

        var requestId = Date.now() + "";

        // UTILS

        function parseMessage(msg) {
            var lines = (msg + "").split("\n");
            var type = (lines[0] + "").trim().toUpperCase();
            var args = Object.create(null);
            var body = "";

            for (var i = 1; i < lines.length; i++) {
                if (lines[i].trim().length === 0) {
                    body = lines.slice(i + 1).join("\n");
                    break;
                }
                var argTxt = lines[i].split(":");
                var key = (argTxt[0] + "").trim().toLowerCase();
                var value = argTxt.slice(1).join(":").trim();
                args[key] = value;
            }

            return {
                type: type,
                args: args,
                body: body,
            };
        }

        function makeMessage(type, args, body) {
            var txt = "" + type;

            for (var key in args) {
                txt += "\n" + key + ": " + args[key];
            }

            if (body) {
                txt += "\n\n" + body;
            }

            return txt;
        }

        function loadFromForm() {
            streamID = document.getElementById("test_stream_id").value;
            authToken = document.getElementById("test_auth").value;

            try {
                rtcConfig = JSON.parse(document.getElementById("test_config").value);
            } catch (ex) {
                console.error(ex);
                rtcConfig = {};
            }
        }

        function escapeHTML(html) {
            return ("" + html).replace(/&/g, "&amp;").replace(/</g, "&lt;")
                .replace(/>/g, "&gt;").replace(/"/g, "&quot;")
                .replace(/'/g, "&apos;");
        };

        function changeStatus(status) {
            document.getElementById("test_status").innerHTML = escapeHTML(status);
        }

        function disableForm() {
            document.getElementById("test_stream_id").disabled = true;
            document.getElementById("test_auth").disabled = true;
            document.getElementById("publish_button").disabled = true;
            document.getElementById("publish_screen_button").disabled = true;
            document.getElementById("watch_button").disabled = true;
            document.getElementById("stop_button").disabled = false;
        }

        function enableForm() {
            document.getElementById("test_stream_id").disabled = false;
            document.getElementById("test_auth").disabled = false;
            document.getElementById("publish_button").disabled = false;
            document.getElementById("publish_screen_button").disabled = false;
            document.getElementById("watch_button").disabled = false;
            document.getElementById("stop_button").disabled = true;
        }

        function getMedia(reqFunc, callback) {
            var options = {
                audio: true,
                video: true,
            };
            // First we try both
            reqFunc(options, function (stream) {
                callback(stream);
            }, function (err) {
                console.error(err);
                if (err.name === "NotAllowedError") {
                    return callback(null, err.message);
                }
                // Now we try only audio
                options = {
                    audio: true,
                    video: false,
                };
                reqFunc(options, function (stream) {
                    callback(stream);
                }, function (err2) {
                    console.error(err2);
                    if (err2.name === "NotAllowedError") {
                        return callback(null, err2.message);
                    }
                    // Now we try only video
                    options = {
                        audio: false,
                        video: true,
                    };
                    reqFunc(options, function (stream) {
                        callback(stream);
                    }, function (err3) {
                        console.error(err3);
                        // No way to get the stream
                        return callback(null, err3.message);
                    });
                });
            });
        }

        function getStreamType(stream) {
            var hasAudio = stream.getAudioTracks().length > 0;
            var hasVideo = stream.getVideoTracks().length > 0;
            if (hasAudio && hasVideo) {
                return "DUAL";
            } else if (hasVideo) {
                return "VIDEO";
            } else {
                return "AUDIO";
            }
        }

        // PUBLISH

        function publishOnOffer(msg) {
            if (peerConnection) {
                peerConnection.close();
                peerConnection = null;
            }

            peerConnection = new RTCPeerConnection(rtcConfig);

            peerConnection.onicecandidate = function (ev) {
                if (ev.candidate) {
                    ws.send(makeMessage("CANDIDATE", {
                        "Request-ID": requestId,
                    }, JSON.stringify(ev.candidate)));
                } else {
                    ws.send(makeMessage("CANDIDATE", {
                        "Request-ID": requestId,
                    }, ""));
                }
            }

            peerConnection.setRemoteDescription(JSON.parse(msg.body)).then(function () {
                // Add tracks
                mediaStream.getTracks().forEach(function (track) {
                    peerConnection.addTrack(track)
                });

                peerConnection.createAnswer().then(function (answer) {
                    console.log(answer);
                    peerConnection.setLocalDescription(answer).then(function () {
                        ws.send(makeMessage("ANSWER", {
                            "Request-ID": requestId,
                        }, JSON.stringify(answer)));
                    });
                })
            });

            peerConnection.onconnectionstatechange = function (ev) {
                switch (peerConnection.connectionState) {
                    case "new":
                    case "checking":
                        console.log("PC: Connecting...");
                        break;
                    case "connected":
                        console.log("PC: Online");
                        break;
                    case "disconnected":
                        console.log("PC: Disconnecting...");
                        break;
                    case "closed":
                        console.log("PC: Offline");
                        break;
                    case "failed":
                        console.log("PC: Error");
                        break;
                    default:
                        console.log("PC: " + peerConnection.connectionState);
                        break;
                }
            };
        }

        function onCandidate(msg) {
            var candidate = msg.body ? JSON.parse(msg.body) : null;

            if (peerConnection) {
                peerConnection.addIceCandidate(candidate);
            }
        }

        function publishStream() {
            if (!streamID) {
                alert("Plaese type a valid stream ID");
                stopClient();
                enableForm();
                return;
            }

            document.getElementById("test_video").muted = true;
            document.getElementById("test_video").srcObject = mediaStream;
            document.getElementById("test_video").play();

            changeStatus("Status: Connecting...");
            ws = new WebSocket((location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws");

            ws.onopen = function () {
                changeStatus("Status: Connected. Publishing stream...");

                ws.send(makeMessage("PUBLISH", {
                    "Request-ID": requestId,
                    "Stream-ID": streamID,
                    "Stream-Type": getStreamType(mediaStream),
                    "Auth": authToken,
                }, ""));
            };

            ws.onclose = function () {
                ws = null;
                stopClient();
                enableForm();
            };

            ws.onerror = function (err) {
                console.error(err);
            };

            ws.addEventListener("message", function (event) {
                var msg = parseMessage("" + event.data);
                console.log(msg);

                switch (msg.type) {
                    case "OK":
                        changeStatus("Status: Connected");
                        break;
                    case "ERROR":
                        stopClient();
                        enableForm();
                        alert("Error: " + msg.args["error-message"]);
                        break;
                    case "OFFER":
                        publishOnOffer(msg);
                        break;
                    case "CANDIDATE":
                        onCandidate(msg);
                        break;
                    case "CLOSE":
                        stopClient();
                        enableForm();
                        break;
                }
            });
        }

        // WATCH

        function watchOnOffer(msg) {
            if (peerConnection) {
                peerConnection.close();
                peerConnection = null;
            }

            if (mediaStream) {
                try {
                    mediaStream.stop();
                } catch (ex) { }
                try {
                    mediaStream.getTracks().forEach(function (track) {
                        track.stop();
                    });
                } catch (ex) { }
            }

            mediaStream = new MediaStream();

            document.getElementById("test_video").muted = false;
            document.getElementById("test_video").srcObject = mediaStream;
            document.getElementById("test_video").play();

            peerConnection = new RTCPeerConnection(rtcConfig);

            peerConnection.onicecandidate = function (ev) {
                if (ev.candidate) {
                    ws.send(makeMessage("CANDIDATE", {
                        "Request-ID": requestId,
                    }, JSON.stringify(ev.candidate)));
                } else {
                    ws.send(makeMessage("CANDIDATE", {
                        "Request-ID": requestId,
                    }, ""));
                }
            }

            peerConnection.ontrack = function (ev) {
                console.log(ev.track);
                mediaStream.addTrack(ev.track);
            };

            peerConnection.setRemoteDescription(JSON.parse(msg.body)).then(function () {
                peerConnection.createAnswer().then(function (answer) {
                    console.log(answer);
                    peerConnection.setLocalDescription(answer).then(function () {
                        ws.send(makeMessage("ANSWER", {
                            "Request-ID": requestId,
                        }, JSON.stringify(answer)));
                    });
                })
            });

            peerConnection.onconnectionstatechange = function (ev) {
                switch (peerConnection.connectionState) {
                    case "new":
                    case "checking":
                        console.log("PC: Connecting...");
                        break;
                    case "connected":
                        console.log("PC: Online");
                        break;
                    case "disconnected":
                        console.log("PC: Disconnecting...");
                        break;
                    case "closed":
                        console.log("PC: Offline");
                        break;
                    case "failed":
                        console.log("PC: Error");
                        break;
                    default:
                        console.log("PC: " + peerConnection.connectionState);
                        break;
                }
            };
        }

        function watchStream() {
            if (!streamID) {
                alert("Plaese type a valid stream ID");
                stopClient();
                enableForm();
                return;
            }

            changeStatus("Status: Connecting...");
            ws = new WebSocket((location.protocol === "https:" ? "wss://" : "ws://") + location.host + "/ws");

            ws.onopen = function () {
                changeStatus("Status: Connected. Pulling stream...");

                ws.send(makeMessage("PLAY", {
                    "Request-ID": requestId,
                    "Stream-ID": streamID,
                    "Auth": authToken,
                }, ""));
            };

            ws.onclose = function () {
                ws = null;
                stopClient();
                enableForm();
            };

            ws.onerror = function (err) {
                console.error(err);
            };

            ws.addEventListener("message", function (event) {
                var msg = parseMessage("" + event.data);
                console.log(msg);

                switch (msg.type) {
                    case "OK":
                        changeStatus("Status: Connected");
                        break;
                    case "ERROR":
                        stopClient();
                        enableForm();
                        alert("Error: " + msg.args["error-message"]);
                        break;
                    case "OFFER":
                        watchOnOffer(msg);
                        break;
                    case "CANDIDATE":
                        onCandidate(msg);
                        break;
                    case "CLOSE":
                        stopClient();
                        enableForm();
                        break;
                }
            });
        }

        // STOP

        function stopClient() {
            if (ws) {
                ws.close();
                ws = null;
            }
            if (peerConnection) {
                peerConnection.close();
                peerConnection = null;
            }
            if (mediaStream) {
                try {
                    mediaStream.stop();
                } catch (ex) { }
                try {
                    mediaStream.getTracks().forEach(function (track) {
                        track.stop();
                    });
                } catch (ex) { }
            }
            changeStatus("Status: Disconnected");
            document.getElementById("test_video").pause();
            document.getElementById("test_video").srcObject = null;
        }

        // ACTIONS

        document.getElementById("publish_button").addEventListener("click", function () {
            disableForm();
            loadFromForm();

            var reqFunc = function (options, callback, errCallback) {
                try {
                    navigator.mediaDevices.getUserMedia(options)
                        .then(callback)
                        .catch(errCallback);
                } catch (ex) {
                    try {
                        navigator.getUserMedia(options)
                            .then(callback)
                            .catch(errCallback);
                    } catch (ex2) {
                        errCallback(ex2);
                    }
                }
            };

            getMedia(reqFunc, function (stream) {
                if (!stream) {
                    enableForm();
                    return;
                }

                mediaStream = stream;

                publishStream();
            });
        });

        document.getElementById("publish_screen_button").addEventListener("click", function () {
            disableForm();
            loadFromForm();

            var reqFunc = function (options, callback, errCallback) {
                try {
                    navigator.mediaDevices.getDisplayMedia(options)
                        .then(callback)
                        .catch(errCallback);
                } catch (ex) {
                    errCallback(ex);
                }
            };

            getMedia(reqFunc, function (stream) {
                if (!stream) {
                    enableForm();
                    return;
                }

                mediaStream = stream;

                publishStream();
            });
        });

        document.getElementById("watch_button").addEventListener("click", function () {
            disableForm();
            loadFromForm();

            if (!streamID) {
                enableForm();
                return;
            }

            mediaStream = null;

            watchStream();
        });

        document.getElementById("stop_button").addEventListener("click", function () {
            stopClient()
            enableForm();
        });

        setInterval(function () {
            if (ws) {
                ws.send(makeMessage("HEARTBEAT", {}, ""));
            }
        }, 20000);

    </script>
</body>

</html>
`
