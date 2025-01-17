FROM golang:1.17.1-buster AS build

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -yqq \
        wget \
        tar \
        git \
        pkg-config \
        build-essential \
        libgstreamer1.0-dev \
        libgstreamer1.0-0 \
        libgstreamer-plugins-base1.0-dev \
        gstreamer1.0-plugins-base \
        gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad \
        gstreamer1.0-plugins-ugly

ENV GO111MODULE=on

WORKDIR /src

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o /out/rtq main.go

FROM ubuntu:20.04

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -yqq \
        libgstreamer1.0-0 \
        gstreamer1.0-plugins-base \
        gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad \
        gstreamer1.0-plugins-ugly \
        gstreamer1.0-libav \
        gstreamer1.0-doc \
        gstreamer1.0-tools \
        gstreamer1.0-x \
        gstreamer1.0-alsa \
        gstreamer1.0-gl \
        gstreamer1.0-gtk3 \
        gstreamer1.0-qt5 \
        gstreamer1.0-pulseaudio \
	iproute2 \
	iputils-ping \
    iperf3 \
    tcpdump \
    python3
 

COPY --from=build \
        /out/rtq \
        /src/tools/run_endpoint.sh \
        ./

ADD link/link.sh ./
ADD tools/tcpdumpbitrate.py ./

RUN chmod +x run_endpoint.sh
RUN chmod +x link.sh

ENTRYPOINT [ "./run_endpoint.sh" ]
