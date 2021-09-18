package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/mengelbart/rtq-go-endpoint/rtc"
	"github.com/mengelbart/rtq-go-endpoint/transport"
)

func init() {
	log.SetFlags(log.Lshortfile)
	rand.Seed(time.Now().UnixNano())
}

const (
	H264 = "h264"
	VP8  = "vp8"
	VP9  = "vp9"

	QUIC = "quic"
	UDP  = "udp"

	NOCC         = "nocc"
	SCREAM       = "scream"
	SCREAM_INFER = "scream-infer"
)

func main() {
	logWriter, err := utils.GetMainLogWriter()
	if err != nil {
		log.Fatal(err)
	}
	defer logWriter.Close()
	log.SetOutput(logWriter)
	defer log.Println("END MAIN")

	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)
	receiveCmd := flag.NewFlagSet("receive", flag.ExitOnError)

	var (
		addr   string
		codec  string
		proto  string
		rtcc   string
		stream bool
	)
	for _, fs := range []*flag.FlagSet{sendCmd, receiveCmd} {
		fs.StringVar(&addr, "addr", ":4242", "addr host the receiver or to connect the sender to")
		fs.StringVar(&codec, "codec", H264, "Video Codec")
		fs.StringVar(&proto, "transport", QUIC, fmt.Sprintf("Transport to use, options: '%v', '%v'", QUIC, UDP))
		fs.StringVar(&rtcc, "cc", NOCC, fmt.Sprintf("Real-time Congestion Controller to use, options: '%v', '%v', '%v'", NOCC, SCREAM, SCREAM_INFER))
		fs.BoolVar(&stream, "stream", false, "send data on a QUIC stream in parallel (only effective if proto=quic)")
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
		send(src, proto, addr, codec, rtcc, stream)
	case "receive":
		receiveCmd.Parse(os.Args[2:])
		files := receiveCmd.Args()
		log.Printf("dst file: %v\n", files)
		dst := "autovideosink"
		if len(files) > 0 {
			dst = fmt.Sprintf("matroskamux ! filesink location=%v", files[0])
		}
		receive(dst, proto, addr, codec, rtcc, stream)
	default:
		fmt.Printf("unknown command: %v\n", os.Args[1])
		fmt.Println("expected 'send' or 'receive' subcommands")
		os.Exit(1)
	}
}

func send(src, proto, remote, codec, rtcc string, stream bool) {
	start := time.Now()

	var w rtc.RTPWriter
	var r io.Reader
	var cancel func() error
	var metricer rtc.Metricer
	switch proto {
	case QUIC:

		var rttTracer *utils.RTTTracer
		if rtcc == SCREAM_INFER {
			rttTracer = utils.NewTracer()
			metricer = rttTracer
		}
		q, err := transport.NewQUICClient(remote, rttTracer)

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

		if stream {
			l, err := utils.GetStreamLogWriter()
			if err != nil {
				log.Fatal(err)
			}
			ctx, cancelCtx := context.WithCancel(context.Background())
			go func() {
				err := sendStreamData(ctx, q, start, l)
				if err != nil {
					log.Fatalf("failed to send stream data: %v", err)
				}
			}()
			cancelTmp := cancel // TODO: Is this necessary?
			cancel = func() error {
				cancelCtx()
				return cancelTmp()
			}
		}

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
		cclog, err := utils.GetCCStatLogWriter()
		if err != nil {
			log.Fatal(err)
		}
		err = sender.ConfigureSCReAMInterceptor(cclog)
		if err != nil {
			log.Fatal(err)
		}
		err = sender.AcceptFeedback()
		if err != nil {
			log.Fatal(err)
		}
	case SCREAM_INFER:
		cclog, err := utils.GetCCStatLogWriter()
		if err != nil {
			log.Fatal(err)
		}
		err = sender.ConfigureInferingSCReAMInterceptor(cclog, w.(rtc.AckingRTPWriter), metricer)
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

func receive(dst, proto, remote, codec, rtcc string, stream bool) {
	start := time.Now()

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

		if stream {
			l, err := utils.GetStreamLogWriter()
			if err != nil {
				log.Fatal(err)
			}
			ctx, cancelCtx := context.WithCancel(context.Background())
			go func() {
				receiveStreamData(ctx, q, start, l)
				if err != nil {
					log.Fatalf("failed to receive stream data: %v", err)
				}
			}()
			cancelTmp := cancel //TODO: is this necessary? (see above, too)
			cancel = func() error {
				cancelCtx()
				return cancelTmp()
			}
		}

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

	recv, err := rtc.NewReceiver(r, w, rtc.ReceiverDst(dst), rtc.ReceiverCodec(codec))
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

type loggingStreamReader struct {
	r              io.Reader
	logger         io.Writer
	sequenceNumber uint64
	start          time.Time
}

func (l *loggingStreamReader) Read(p []byte) (n int, err error) {
	n, err = l.r.Read(p)
	fmt.Fprintf(l.logger, "%v, %v, %v\n", time.Since(l.start).Milliseconds(), l.sequenceNumber, n)
	return
}

func receiveStreamData(ctx context.Context, q *transport.QUIC, start time.Time, logger io.WriteCloser) error {
	stream, err := q.AcceptUniStream(ctx)
	if err != nil {
		return err
	}
	defer logger.Close()

	var nextPacket streamDataPacket

	lsr := loggingStreamReader{
		r:              stream,
		logger:         logger,
		sequenceNumber: 0,
		start:          start,
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:

			headerReader := io.LimitReader(&lsr, 16)
			headerBytes, err := ioutil.ReadAll(headerReader)
			if err != nil {
				return err
			}
			if len(headerBytes) != 16 {
				return fmt.Errorf("read wrong number of bytes from header, got: %v but want: %v", len(headerBytes), 16)
			}
			nextPacket.UnmarshalBinary(headerBytes)

			if nextPacket.sequenceNumber != lsr.sequenceNumber {
				log.Fatalf("received unexpected sequenceNumber, expected: %v, got %v\n", nextPacket.sequenceNumber, lsr.sequenceNumber)
			}

			bodyReader := io.LimitReader(&lsr, int64(nextPacket.length))
			bodyBytes, err := ioutil.ReadAll(bodyReader)
			if err != nil {
				return err
			}
			if uint64(len(bodyBytes)) != nextPacket.length {
				return fmt.Errorf("read wrong number of bytes from body, got: %v but want: %v", len(bodyBytes), nextPacket.length)
			}

			lsr.sequenceNumber++
		}
	}
}

//const streamDataPacketLength = 1484
const streamDataPacketLength = 64_000

func sendStreamData(ctx context.Context, q *transport.QUIC, start time.Time, logger io.WriteCloser) error {
	stream, err := q.OpenUniStream()
	if err != nil {
		return err
	}
	defer stream.Close()
	defer logger.Close()

	nextSequenceNumber := uint64(0)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			p := streamDataPacket{
				sequenceNumber: nextSequenceNumber,
				length:         streamDataPacketLength,
				data:           make([]byte, streamDataPacketLength),
			}
			_, err := rand.Read(p.data)
			if err != nil {
				return err
			}
			bs, err := p.MarshalBinary()
			if err != nil {
				return err
			}
			nextSequenceNumber++

			n, err := stream.Write(bs)
			if err != nil {
				return err
			}
			fmt.Fprintf(logger, "%v, %v, %v\n", time.Since(start).Milliseconds(), p.sequenceNumber, n)
		}
	}
}

type streamDataPacket struct {
	sequenceNumber uint64
	length         uint64 // length of following data in bytes
	data           []byte
}

func (p *streamDataPacket) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, p.sequenceNumber)
	if err != nil {
		return nil, err
	}
	err = binary.Write(buf, binary.BigEndian, uint64(len(p.data)))
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(p.data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *streamDataPacket) UnmarshalBinary(data []byte) error {
	if len(data) < 16 {
		return fmt.Errorf("protocol packet too short: got %v bytes, expected at least 8 bytes", len(data))
	}
	p.sequenceNumber = binary.BigEndian.Uint64(data[0:8])
	p.length = binary.BigEndian.Uint64(data[8:16])
	p.data = make([]byte, p.length)
	return nil
}

func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}
