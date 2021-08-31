package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/signal"

	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/mengelbart/rtq-go-endpoint/rtc"
	"github.com/mengelbart/rtq-go-endpoint/transport"
)

func init() {
	log.SetFlags(log.Lshortfile)
}

const (
	H264 = "h264"
	VP8  = "vp8"
	VP9  = "vp9"

	QUIC = "quic"
	UDP  = "udp"

	NOCC   = "nocc"
	SCREAM = "scream"
)

func main() {
	logFilename := os.Getenv("LOG_FILE")
	if logFilename != "" {
		logfile, err := os.Create(logFilename)
		if err != nil {
			fmt.Printf("Could not create log file: %s\n", err.Error())
			os.Exit(1)
		}
		defer logfile.Close()
		log.SetOutput(logfile)
	}
	defer log.Println("END MAIN")

	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)
	receiveCmd := flag.NewFlagSet("receive", flag.ExitOnError)

	var (
		addr  string
		codec string
		proto string
		rtcc  string
	)
	for _, fs := range []*flag.FlagSet{sendCmd, receiveCmd} {
		fs.StringVar(&addr, "addr", ":4242", "addr host the receiver or to connect the sender to")
		fs.StringVar(&codec, "codec", H264, "Video Codec")
		fs.StringVar(&proto, "transport", QUIC, fmt.Sprintf("Transport to use, options: '%v', '%v'", QUIC, UDP))
		fs.StringVar(&rtcc, "cc", NOCC, fmt.Sprintf("Real-time Congestion Controller to use, options: '%v', '%v'", NOCC, SCREAM))
	}

	log.Println(os.Args)

	if len(os.Args) < 2 {
		fmt.Println("expected 'send' or 'receive' subcommands")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "send":
		sendCmd.Parse(os.Args[2:])
		files := sendCmd.Args()
		log.Printf("src files: %v\n", files)
		src := "videotestsrc"
		if len(files) > 0 {
			src = fmt.Sprintf("filesrc location=%v ! queue ! decodebin ! videoconvert ", files[0])
		}
		send(src, proto, addr, codec, rtcc)
	case "receive":
		receiveCmd.Parse(os.Args[2:])
		files := receiveCmd.Args()
		log.Printf("dst file: %v\n", files)
		dst := "autovideosink"
		if len(files) > 0 {
			dst = fmt.Sprintf("matroskamux ! filesink location=%v", files[0])
		}
		receive(dst, proto, addr, codec, rtcc)
	default:
		fmt.Printf("unknown command: %v\n", os.Args[1])
		fmt.Println("expected 'send' or 'receive' subcommands")
		os.Exit(1)
	}
}

func send(src, proto, remote, codec, rtcc string) {
	var w rtc.RTPWriter
	var r io.Reader
	var cancel func() error
	switch proto {
	case QUIC:

		q, err := transport.NewQUICClient(remote)
		if err != nil {
			log.Fatalf("failed to open RTQ session: %v", err)
		}
		w, err = q.Writer(0)
		if err != nil {
			log.Fatalf("failed to open RTQ write flow: %v", err)
		}
		r, err = q.Reader(1)
		if err != nil {
			log.Fatalf("failed to open RTQ read flow: %v", err)
		}
		cancel = q.Close

	case UDP:

		q, err := transport.NewUDPClient(remote)
		if err != nil {
			log.Fatalf("failed to open UDP session: %v", err)
		}
		wr, err := q.Writer(0)
		if err != nil {
			log.Fatalf("failed to open UDP write flow: %v", err)
		}
		r, err = q.Reader(1)
		if err != nil {
			log.Fatalf("failed to open UDP read flow: %v", err)
		}
		w = wr
		cancel = wr.Close

	default:
		log.Fatalf("unknown transport protocol: %v", proto)
	}

	sender, err := rtc.NewSender(
		w,
		r,
		rtc.SenderCodec(codec),
		rtc.SenderSrc(src),
	)
	if err != nil {
		log.Fatalf("failed to create RTP sender: %v", err)
	}

	rtpLogger, err := utils.GetRTPLogWriter()
	if err != nil {
		log.Fatal(err)
	}
	sender.ConfigureRTPLogInterceptor(rtpLogger("rtcp_in"), utils.NopCloser{Writer: ioutil.Discard}, utils.NopCloser{Writer: ioutil.Discard}, rtpLogger("rtp_out"))

	switch rtcc {
	case SCREAM:
		sender.ConfigureSCReAMInterceptor()
		err := sender.AcceptFeedback()
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Printf("unknown cc: %v\n", rtcc)
	}

	done := make(chan struct{})
	sender.NotifyDone(done)
	err = sender.Start()
	if err != nil {
		log.Fatalf("failed to start RTP sender: %v", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	select {
	case sig := <-signals:
		log.Printf("got signal: %v, closing sender", sig)
	case <-done:
		log.Printf("reached EOS, closing sender")
	}
	err = sender.Close()
	if err != nil {
		log.Fatalf("failed to close sender %v", err)
	}
	err = cancel()
	if err != nil {
		log.Fatalf("failed to close transport %v", err)
	}
}

func receive(dst, proto, remote, codec, rtcc string) {
	var w rtc.RTCPWriter
	var r io.Reader
	var cancel func() error
	switch proto {
	case QUIC:

		q, err := transport.NewQUICServer(remote)
		if err != nil {
			log.Fatalf("failed to open RTQ session: %v", err)
		}
		r, err = q.Reader(0)
		if err != nil {
			log.Fatalf("failed to open RTQ read flow: %v", err)
		}
		w, err = q.Writer(1)
		if err != nil {
			log.Fatalf("failed to open RTQ write flow: %v", err)
		}
		cancel = q.Close

	case UDP:

		u, err := transport.NewUDPServer(remote)
		if err != nil {
			log.Fatalf("failed to open UDP session: %v", err)
		}
		rr, err := u.Reader(0)
		if err != nil {
			log.Fatalf("failed to open UDP read flow: %v", err)
		}
		w, err = u.Writer(1)
		if err != nil {
			log.Fatalf("failed to open UDP write flow: %v", err)
		}
		r = rr
		cancel = rr.Close

	default:
		log.Fatalf("unknown transport protocol: %v", proto)
	}

	recv, err := rtc.NewReceiver(r, w, rtc.ReceiverDst(dst))
	if err != nil {
		log.Fatalf("failed to create RTP receiver: %v", err)
	}

	rtpLogger, err := utils.GetRTPLogWriter()
	if err != nil {
		log.Fatal(err)
	}
	recv.ConfigureRTPLogInterceptor(utils.NopCloser{Writer: ioutil.Discard}, rtpLogger("rtcp_out"), rtpLogger("rtp_in"), utils.NopCloser{Writer: ioutil.Discard})

	switch rtcc {
	case SCREAM:
		recv.ConfigureSCReAMInterceptor()
	default:
		log.Printf("unknown cc: %v\n", rtcc)
	}

	done := make(chan struct{})
	recv.NotifyDone(done)
	err = recv.Receive()
	if err != nil {
		log.Fatalf("failed to start RTP receiver: %v", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	select {
	case sig := <-signals:
		log.Printf("got signal: %v, closing receiver", sig)
		err = recv.Close()
		if err != nil {
			log.Fatalf("failed to close receiver %v", err)
		}
		err = cancel()
		if err != nil {
			log.Fatalf("failed to close transport: %v", err)
		}
	case <-done:
		log.Printf("reached EOS, closing receiver")
	}
}
