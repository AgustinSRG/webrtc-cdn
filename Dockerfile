FROM golang:latest

WORKDIR /root

# Copy files

ADD . /root

# Fetch dependencies

RUN go get .

# Compile

RUN go build

# Expose ports

EXPOSE 80
EXPOSE 443

EXPOSE 40000:65535/UDP

# Entry point

ENTRYPOINT ["/root/webrtc-cdn"]
