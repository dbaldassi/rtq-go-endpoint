package endpoint

const (
	RTQTransport = "quic"
	UDPTransport = "udp"
)

const (
	H264 = "h264"
	VP8  = "vp8"
	VP9  = "vp9"
)

// Sender is the interface to something that sends RTP packets over some
// network
type Sender interface {
	Send(src string) error
}

// Receiver is the interface to something that receives RTP packets over some
// network
type Receiver interface {
	Receive(dst string) error
}
