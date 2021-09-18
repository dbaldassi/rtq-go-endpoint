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

	"github.com/lucas-clemente/quic-go/logging"
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
		fs.StringVar(&codec, "codec", H264, fmt.Sprintf("Video Codec, options: '%v', '%v', '%v'", H264, VP8, VP9))
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
		if err := sendCmd.Parse(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		files := sendCmd.Args()
		log.Printf("src files: %v\n", files)
		src := "videotestsrc"
		if len(files) > 0 {
			src = fmt.Sprintf("filesrc location=%v ! queue ! decodebin ! videoconvert ", files[0])
		}
		if err := send(src, proto, addr, codec, rtcc, stream); err != nil {
			log.Fatal(err)
		}
	case "receive":
		if err := receiveCmd.Parse(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		files := receiveCmd.Args()
		log.Printf("dst file: %v\n", files)
		dst := "autovideosink"
		if len(files) > 0 {
			dst = fmt.Sprintf("matroskamux ! filesink location=%v", files[0])
		}
		if err := receive(dst, proto, addr, codec, rtcc, stream); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Printf("unknown command: %v\n", os.Args[1])
		fmt.Println("expected 'send' or 'receive' subcommands")
		os.Exit(1)
	}
}

func closeErr(closeFn func() error) {
	if err := closeFn(); err != nil {
		log.Printf("close failed: %v\n", err)
	}
}

func send(src, proto, remote, codec, rtcc string, stream bool) error {
	start := time.Now()

	var w rtc.RTPWriter
	var r io.Reader
	var metricer rtc.Metricer

	switch proto {
	case QUIC:

		var tracers []logging.Tracer
		if rtcc == SCREAM_INFER {
			rttTracer := utils.NewTracer()
			tracers = append(tracers, rttTracer)
			metricer = rttTracer
		}
		q, err := transport.NewQUICClient(remote, tracers...)
		if err != nil {
			return fmt.Errorf("failed to open RTQ session: %v", err)
		}
		defer closeErr(q.Close)

		writeCloser, err := q.Writer(0)
		if err != nil {
			return fmt.Errorf("failed to open RTQ write flow: %v", err)
		}
		defer closeErr(writeCloser.Close)
		w = writeCloser

		readCloser, err := q.Reader(1)
		if err != nil {
			return fmt.Errorf("failed to open RTQ read flow: %v", err)
		}
		defer closeErr(readCloser.Close)
		r = readCloser

		if stream {
			l, err := utils.GetStreamLogWriter()
			if err != nil {
				return fmt.Errorf("failed to get stream log writer: %v", err)
			}
			defer closeErr(l.Close)

			ctx, cancelCtx := context.WithCancel(context.Background())
			go func() {
				err := sendStreamData(ctx, q, start, l)
				if err != nil && err.Error() == "Application error 0x0: eos" {
					log.Printf("stream sender done after EOS")
					return
				}
				if err != nil {
					log.Fatalf("failed to send stream data: %v", err) // TODO: return error to main goroutine
				}
			}()
			defer cancelCtx()
		}

	case UDP:

		u, err := transport.NewUDPClient(remote)
		if err != nil {
			return fmt.Errorf("failed to open UDP session: %v", err)
		}
		defer closeErr(u.Close)

		writeCloser, err := u.Writer(0)
		if err != nil {
			return fmt.Errorf("failed to open UDP write flow: %v", err)
		}
		defer closeErr(writeCloser.Close)
		w = writeCloser

		readCloser, err := u.Reader(1)
		if err != nil {
			return fmt.Errorf("failed to open UDP read flow: %v", err)
		}
		defer closeErr(readCloser.Close)
		r = readCloser

	default:
		return fmt.Errorf("unknown transport protocol: %v", proto)
	}

	rtpLogger, err := utils.GetRTPLogWriter()
	if err != nil {
		return fmt.Errorf("failed to get RTP log writer: %v", err)
	}
	rtcpInLog := rtpLogger("rtcp_in")
	rtpOutLog := rtpLogger("rtp_out")
	defer closeErr(rtcpInLog.Close)
	defer closeErr(rtpOutLog.Close)

	sender, err := rtc.NewSender(
		w,
		r,
		rtc.SenderCodec(codec),
		rtc.SenderSrc(src),
	)
	if err != nil {
		return fmt.Errorf("failed to create RTP sender: %v", err)
	}
	defer closeErr(sender.Close)

	sender.ConfigureRTPLogInterceptor(rtcpInLog, ioutil.Discard, ioutil.Discard, rtpOutLog)

	switch rtcc {
	case SCREAM:
		cclog, err := utils.GetCCStatLogWriter()
		if err != nil {
			return fmt.Errorf("failed to get CC stats log writer: %v", err)
		}
		defer closeErr(cclog.Close)

		err = sender.ConfigureSCReAMInterceptor(cclog)
		if err != nil {
			return fmt.Errorf("failed to configure SCReAM interceptor: %v", err)
		}
		err = sender.AcceptFeedback() // TODO: Check if goroutine needs explicit close
		if err != nil {
			return fmt.Errorf("failed to start SCReAM feedback acceptor: %v", err)
		}

	case SCREAM_INFER:
		cclog, err := utils.GetCCStatLogWriter()
		if err != nil {
			return fmt.Errorf("failed to get CC stats log writer: %v", err)
		}
		defer closeErr(cclog.Close)

		err = sender.ConfigureInferingSCReAMInterceptor(cclog, w.(rtc.AckingRTPWriter), metricer)
		if err != nil {
			return fmt.Errorf("failed to configure inferring SCReAM interceptor: %v", err)
		}

	default:
		log.Printf("unknown cc: %v\n", rtcc)
	}

	done := make(chan struct{})
	errChan := make(chan error)
	go func() {
		err = sender.Start()
		if err != nil {
			errChan <- fmt.Errorf("failed to start RTP sender: %v", err)
			return
		}
		close(done)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	select {
	case sig := <-signals:
		log.Printf("got signal: %v, closing sender", sig)

	case <-done:
		log.Printf("reached EOS, closing sender")

	case err := <-errChan:
		return err
	}

	return nil
}

func receive(dst, proto, remote, codec, rtcc string, stream bool) error {
	start := time.Now()

	var w rtc.RTCPWriter
	var r io.Reader

	switch proto {
	case QUIC:

		q, err := transport.NewQUICServer(remote)
		if err != nil {
			return fmt.Errorf("failed to open RTQ session: %v", err)
		}
		defer closeErr(q.Close)

		readCloser, err := q.Reader(0)
		if err != nil {
			return fmt.Errorf("failed to open RTQ read flow: %v", err)
		}
		defer closeErr(readCloser.Close)
		r = readCloser

		writeCloser, err := q.Writer(1)
		if err != nil {
			return fmt.Errorf("failed to open RTQ write flow: %v", err)
		}
		defer closeErr(writeCloser.Close)
		w = writeCloser

		if stream {
			l, err := utils.GetStreamLogWriter()
			if err != nil {
				return fmt.Errorf("failed to get stream log writer: %v", err)
			}
			defer closeErr(l.Close)

			ctx, cancelCtx := context.WithCancel(context.Background())
			go func() {
				if err := receiveStreamData(ctx, q, start, l); err != nil {
					log.Fatalf("failed to receive stream data: %v", err) // TODO: return error to main goroutine
				}
			}()
			defer cancelCtx()
		}

	case UDP:

		u, err := transport.NewUDPServer(remote)
		if err != nil {
			return fmt.Errorf("failed to open UDP session: %v", err)
		}
		defer closeErr(u.Close)

		readCloser, err := u.Reader(0)
		if err != nil {
			return fmt.Errorf("failed to open UDP read flow: %v", err)
		}
		defer closeErr(readCloser.Close)
		r = readCloser

		writeCloser, err := u.Writer(1)
		if err != nil {
			return fmt.Errorf("failed to open UDP write flow: %v", err)
		}
		defer closeErr(writeCloser.Close)
		w = writeCloser

	default:
		return fmt.Errorf("unknown transport protocol: %v", proto)
	}

	rtpLogger, err := utils.GetRTPLogWriter()
	if err != nil {
		return fmt.Errorf("failed to get RTP log writer: %v", err)
	}
	rtcpOutLog := rtpLogger("rtcp_out")
	rtpInLog := rtpLogger("rtp_in")
	defer closeErr(rtcpOutLog.Close)
	defer closeErr(rtpInLog.Close)

	recv, err := rtc.NewReceiver(r, w, rtc.ReceiverDst(dst), rtc.ReceiverCodec(codec))
	if err != nil {
		return fmt.Errorf("failed to create RTP receiver: %v", err)
	}

	recv.ConfigureRTPLogInterceptor(ioutil.Discard, rtcpOutLog, rtpInLog, ioutil.Discard)

	if rtcc == SCREAM {
		err := recv.ConfigureSCReAMInterceptor()
		if err != nil {
			return fmt.Errorf("failed to configure SCReAM interceptor: %v", err)
		}
	}

	done := make(chan struct{})
	errChan := make(chan error)

	go func() {
		err = recv.Receive()
		if err != nil {
			errChan <- fmt.Errorf("failed to start RTP receiver: %v", err)
			return
		}
		close(done)
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	select {
	case sig := <-signals:
		log.Printf("got signal: %v, closing receiver", sig)

	case <-done:
		log.Printf("reached EOS, closing receiver")

	case err := <-errChan:
		return err
	}

	return nil
}

func receiveStreamData(ctx context.Context, q *transport.QUIC, start time.Time, logger io.Writer) error {
	stream, err := q.AcceptUniStream(ctx)
	if err != nil {
		return err
	}

	buffer := make([]byte, streamDataPacketLength)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			n, err := stream.Read(buffer)
			if err != nil {
				return err
			}
			fmt.Fprintf(logger, "%v, %v\n", time.Since(start).Milliseconds(), n)
		}
	}
}

const streamDataPacketLength = 1400

//const streamDataPacketLength = 64_000

func sendStreamData(ctx context.Context, q *transport.QUIC, start time.Time, logger io.Writer) error {
	stream, err := q.OpenUniStream()
	if err != nil {
		return err
	}
	defer stream.Close()

	buffer := make([]byte, streamDataPacketLength)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:

			_, err := rand.Read(buffer)
			if err != nil {
				return err
			}
			n, err := stream.Write(buffer)
			if err != nil {
				return err
			}
			fmt.Fprintf(logger, "%v, %v\n", time.Since(start).Milliseconds(), n)
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
