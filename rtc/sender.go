package rtc

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	gstsrc "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
	"github.com/mengelbart/rtq-go-endpoint/internal/scream"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	screamcgo "github.com/mengelbart/scream-go"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type RTPWriter interface {
	WriteRTP(header *rtp.Header, payload []byte) (int, error)
}

type AckingRTPWriter interface {
	WriteRTPNotify(header *rtp.Header, payload []byte, notify func(bool)) (int, error)
}

type Sender struct {
	codec string
	src   string
	mtu   int

	writeRTP interceptor.RTPWriterFunc

	rtcpConn io.Reader

	streamInfo *interceptor.StreamInfo
	ir         interceptor.Registry
	i          interceptor.Interceptor

	rtpWriter  interceptor.RTPWriter
	rtcpReader interceptor.RTCPReader

	pipeline *gstsrc.Pipeline

	closeC       chan struct{}
	feedbackErrC chan error
}

type SenderOption func(*Sender) error

func SenderCodec(codec string) SenderOption {
	return func(s *Sender) error {
		s.codec = codec
		return nil
	}
}

func SenderSrc(src string) SenderOption {
	return func(s *Sender) error {
		s.src = src
		return nil
	}
}

func NewSender(w RTPWriter, r io.Reader, opts ...SenderOption) (*Sender, error) {
	s := &Sender{
		codec:    "h264",
		src:      "videotestsrc",
		mtu:      1200,
		writeRTP: defaultRTPWriterFunc(w),
		rtcpConn: r,
		streamInfo: &interceptor.StreamInfo{
			SSRC: 0,
		},
		ir: interceptor.Registry{},

		feedbackErrC: make(chan error),
		closeC:       make(chan struct{}),
	}
	for _, opt := range opts {
		err := opt(s)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Sender) ConfigureInferingSCReAMInterceptor(statsLogger io.Writer, w AckingRTPWriter, m Metricer, inferFromSmoothedRTT bool) error {
	fbc := make(chan []byte, 1_000_000)
	s.inferFeedback(fbc)

	fbi := newFBInferer(w, screamcgo.NewRx(0), fbc, m, inferFromSmoothedRTT)
	s.writeRTP = fbi.rtpWriterFunc
	go fbi.buffer(s.closeC)

	tx := screamcgo.NewTx()
	cc, err := scream.NewSenderInterceptor(scream.Tx(tx))
	if err != nil {
		return err
	}
	s.streamInfo.RTCPFeedback = append(s.streamInfo.RTCPFeedback, interceptor.RTCPFeedback{
		Type:      "ack",
		Parameter: "ccfb",
	})
	s.ir.Add(cc)
	go s.runSCReAMStats(statsLogger, cc, m)
	return nil
}

func (s *Sender) inferFeedback(fbc <-chan []byte) {
	go func() {
		defer log.Println("finish infering feedback")
		for {
			select {
			case buffer := <-fbc:
				if _, _, err := s.rtcpReader.Read(buffer, interceptor.Attributes{}); err != nil {
					s.feedbackErrC <- err
				}
			case <-s.closeC:
				return
			}
		}
	}()
}

func (s *Sender) ConfigureNaiveBitrateAdaption(statsLogger io.Writer, m Metricer) error {
	i, err := utils.NewSenderInterceptor()
	if err != nil {
		return err
	}
	s.ir.Add(i)
	go s.runSCReAMStats(statsLogger, i, m)
	return nil
}

func (s *Sender) ConfigureSCReAMInterceptor(statsLogger io.Writer, m Metricer) error {
	tx := screamcgo.NewTx()
	cc, err := scream.NewSenderInterceptor(scream.Tx(tx))
	if err != nil {
		return err
	}
	s.streamInfo.RTCPFeedback = append(s.streamInfo.RTCPFeedback, interceptor.RTCPFeedback{
		Type:      "ack",
		Parameter: "ccfb",
	})
	s.ir.Add(cc)
	go s.runSCReAMStats(statsLogger, cc, m)
	return nil
}

func (s *Sender) AcceptFeedback() error {
	if s.rtcpConn == nil {
		return fmt.Errorf("cannot read rtcp with nil reader")
	}
	go func() {
		defer log.Println("finish accepting feedback")
		for buffer := make([]byte, s.mtu); ; {
			n, err := s.rtcpConn.Read(buffer)
			if err != nil {
				s.feedbackErrC <- err
			}
			if _, _, err := s.rtcpReader.Read(buffer[:n], interceptor.Attributes{}); err != nil {
				s.feedbackErrC <- err
			}
		}
	}()
	return nil
}

type congestionController interface {
	GetTargetBitrate(ssrc uint32) (float64, error)
	GetStatistics() string
}

func (s *Sender) runSCReAMStats(statsLogger io.Writer, cc congestionController, m Metricer) {
	ticker := time.NewTicker(20 * time.Millisecond)
	start := time.Now()
	var lastBitrate uint
	for {
		select {
		case <-ticker.C:
			bps, err := cc.GetTargetBitrate(0)
			if err != nil {
				log.Printf("failed to get target bitrate: %v\n", err)
			}
			t := time.Since(start).Milliseconds()
			if bps > 0 && s.pipeline != nil && lastBitrate != uint(bps) {
				lastBitrate = uint(bps)
				s.pipeline.SetBitRate(lastBitrate)
			}
			// queueDelay, queueDelayMax, queueDelayMinAvg, sRtt, cwnd,
			// bytesInFlight, rateTransmitted, isInFastStart,
			// rtpQueueDelay, targetBitrate, rateRtp, rateTransmitted,
			// rateAcked, rateLost, rateCe, hiSeqAck
			stats := cc.GetStatistics()
			// time, bitrate, stats

			var metrics = m.Metrics()
			log.Printf("bitrate: %v %v\n", t, lastBitrate/1000)
			log.Printf("QUIC_stats: %v %v %v %v %v %v %v\n",
				metrics.LatestRTT.Seconds(),
				metrics.PacketLoss,
				metrics.PacketDropped,
				metrics.SlowStart,
				metrics.Recovery,
				metrics.CongestionAvoidance,
				metrics.ApplicationLimited)
			log.Printf("SCReAM_stats: %v\n", stats)
		case <-s.closeC:
			return
		}
	}
}

func (s *Sender) ConfigureRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut io.Writer) {
	i := utils.NewRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut)
	s.ir.Add(i)
}

func (s *Sender) Write(p []byte) (n int, err error) {
	var pkt rtp.Packet
	err = pkt.Unmarshal(p)
	if err != nil {
		return 0, err
	}
	_, err = s.rtpWriter.Write(&pkt.Header, pkt.Payload, nil)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func defaultRTPWriterFunc(w RTPWriter) interceptor.RTPWriterFunc {
	return func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		n, err := w.WriteRTP(header, payload)

		if err != nil {
			if netErr, ok := err.(net.Error); ok && !netErr.Temporary() || err.Error() == "Application error 0x0: eos" {
				return n, err
			}
			log.Printf("failed to write to rtpWriter: %T: %v\n", err, err)
		}

		return n, err
	}
}

func (s *Sender) Start() error {
	i := s.ir.Build()
	s.i = i

	s.rtpWriter = s.i.BindLocalStream(s.streamInfo, interceptor.RTPWriterFunc(s.writeRTP))

	s.rtcpReader = s.i.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	errC := make(chan error)

	pipeline, err := gstsrc.NewPipeline(s.codec, s.src, s)
	if err != nil {
		return err
	}
	s.pipeline = pipeline

	eosC := make(chan struct{})
	gstsrc.HandleSrcEOS(func() {
		close(eosC)
	})

	s.pipeline.SetSSRC(uint(s.streamInfo.SSRC))
	s.pipeline.Start()

	go gstsrc.StartMainLoop()

	select {
	case <-eosC:
		log.Println("eos")
	case err := <-errC:
		log.Printf("got error from interceptorWriter: %v\n", err)
		s.pipeline.Stop()
	case err := <-s.feedbackErrC:
		log.Printf("got error from feedback Acceptor: %v\n", err)
		s.pipeline.Stop()
	case <-s.closeC:
		s.pipeline.Stop()
	}
	s.i.Close()
	select {
	case <-eosC:
	case <-time.After(3 * time.Second):
		log.Printf("timeout")
	}

	return nil
}

func (s *Sender) Close() error {
	close(s.closeC)
	return nil
}
