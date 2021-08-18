package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/qlog"
	endpoint "github.com/mengelbart/rtq-go-endpoint"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/mengelbart/rtq-go-endpoint/rtq"
	"github.com/mengelbart/rtq-go-endpoint/udp"
)

func main() {
	logFilename := os.Getenv("LOG_FILE")
	if logFilename != "" {
		logfile, err := os.Create(logFilename)
		if err != nil {
			panic(fmt.Errorf("could not create log file: %w", err))
		}
		defer logfile.Close()
		log.SetOutput(logfile)
	}

	codec := flag.String("codec", endpoint.H264, "Video Codec")
	transport := flag.String("transport", endpoint.RTQTransport, fmt.Sprintf("Transport to use, options: '%v', '%v'", endpoint.RTQTransport, endpoint.UDPTransport))
	cc := flag.String("cc", "", fmt.Sprintf("Congestion Controller to use, options: '%v', '%v'", "", rtq.SCReAM))
	remote := flag.String("remote", ":4242", "remote host to connect and send video to")
	flag.Parse()

	files := flag.Args()

	log.Printf("args: %v\n", files)
	src := "videotestsrc"
	if len(files) > 0 {
		src = fmt.Sprintf("filesrc location=%v ! queue ! decodebin ! videoconvert ", files[0])
	}

	var s endpoint.Sender
	var err error

	switch *transport {
	case endpoint.RTQTransport:
		s, err = setupRTQSender(*remote, *cc, *codec)
		if err != nil {
			log.Fatal(err)
		}
	case endpoint.UDPTransport:
		rtpLogger, err := utils.GetRTPLogWriter()
		if err != nil {
			log.Fatal(err)
		}
		s, err = udp.NewSender(
			*remote,
			udp.SenderCodec(*codec),
			udp.SenderCongestionControl(*cc),
			udp.SenderRTCPInLogWriter(rtpLogger("rtcp_in")),
			udp.SenderRTCPOutLogWriter(utils.NopCloser{Writer: io.Discard}),
			udp.SenderRTPInLogWriter(utils.NopCloser{Writer: io.Discard}),
			udp.SenderRTPOutLogWriter(rtpLogger("rtp_out")),
		)
		if err != nil {
			log.Printf("could not setup sender: %v\n", err)
			os.Exit(1)
		}
	default:
		log.Fatalf("unknown transport: %v", *transport)
	}

	err = s.Send(src)
	if err != nil {
		log.Printf("Could not run sender: %v\n", err.Error())
		os.Exit(1)
	}
}

func setupRTQSender(remote, cc, codec string) (*rtq.Sender, error) {
	qlogWriter, err := utils.GetQLOGWriter()
	if err != nil {
		return nil, fmt.Errorf("could not get qlog writer: %w", err)
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-echo-example"},
	}

	quicConf := &quic.Config{
		EnableDatagrams: true,
	}
	if qlogWriter != nil {
		quicConf.Tracer = qlog.NewTracer(qlogWriter)
	}
	rtpLogger, err := utils.GetRTPLogWriter()
	if err != nil {
		return nil, fmt.Errorf("could not get rtp/rtcp log writer: %w", err)
	}
	return rtq.NewSender(
		remote,
		tlsConf,
		quicConf,
		rtq.SenderCodec(codec),
		rtq.SenderCongestionControl(cc),
		rtq.SenderRTCPInLogWriter(rtpLogger("rtcp_in")),
		rtq.SenderRTCPOutLogWriter(utils.NopCloser{Writer: io.Discard}),
		rtq.SenderRTPInLogWriter(utils.NopCloser{Writer: io.Discard}),
		rtq.SenderRTPOutLogWriter(rtpLogger("rtp_out")),
	)
}
