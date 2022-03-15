FROM golang:latest

WORKDIR /root

# Copy files

ADD . /root

# Fetch dependencies

RUN go get github.com/AgustinSRG/webrtc-cdn

# Compile

RUN go build

# Expose ports

EXPOSE 1935
EXPOSE 443

# Entry point

ENTRYPOINT ["/root/webrtc-cdn"]
