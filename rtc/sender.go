package rtc

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	gstsrc "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/scream"
	"github.com/pion/rtp"
)

type RTPWriteCloser interface {
	WriteRTP(header *rtp.Header, payload []byte) (int, error)
	Close() error
}

type RTCPReadCloser interface {
	io.Reader
	io.Closer
}

type Sender struct {
	codec string
	src   string
	mtu   int

	rtpConn  RTPWriteCloser
	rtcpConn RTCPReadCloser

	streamInfo   *interceptor.StreamInfo
	interceptors interceptor.Registry
	rtpWriter    interceptor.RTPWriter
	rtcpReader   interceptor.RTCPReader

	pipeline *gstsrc.Pipeline

	rtpConnClosed bool
	closed        chan struct{}
}

type SenderOption func(*Sender) error

func SenderCodec(codec string) SenderOption {
	return func(s *Sender) error {
		s.codec = codec
		return nil
	}
}

func NewSender(w RTPWriteCloser, r RTCPReadCloser, opts ...SenderOption) (*Sender, error) {
	s := &Sender{
		codec:    "h264",
		src:      "videotestsrc",
		mtu:      1400,
		rtpConn:  w,
		rtcpConn: r,
		streamInfo: &interceptor.StreamInfo{
			SSRC: 0,
		},
		interceptors: interceptor.Registry{},
		closed:       make(chan struct{}),
	}
	for _, opt := range opts {
		err := opt(s)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Sender) ConfigureSCReAMInterceptor() error {
	cc, err := scream.NewSenderInterceptor()
	if err != nil {
		return err
	}
	s.streamInfo.RTCPFeedback = append(s.streamInfo.RTCPFeedback, interceptor.RTCPFeedback{
		Type:      "ack",
		Parameter: "ccfb",
	})
	s.interceptors.Add(cc)
	return nil
}

func (s *Sender) ConfigureRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut io.WriteCloser) {
	i := utils.NewRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut)
	s.interceptors.Add(i)
}

func (s *Sender) Write(p []byte) (n int, err error) {
	if s.rtpConnClosed {
		log.Println("invalid write on closed sender")
		return len(p), nil
	}
	var pkt rtp.Packet
	err = pkt.Unmarshal(p)
	if err != nil {
		return 0, err
	}
	return s.rtpWriter.Write(&pkt.Header, p[pkt.Header.MarshalSize():], nil)
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
				if err != io.EOF {
					log.Println(err)
				}
				return
			}
			if _, _, err := s.rtcpReader.Read(buffer[:n], interceptor.Attributes{}); err != nil {
				log.Println(err)
				return
			}
		}
	}()
	return nil
}

func (s *Sender) Start() error {
	i := s.interceptors.Build()

	s.rtpWriter = i.BindLocalStream(s.streamInfo, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		n, err := s.rtpConn.WriteRTP(header, payload)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && !netErr.Temporary() || err.Error() == "Application error 0x0: eos" {
				fmt.Printf("err: %v, closing pipeline", err)
				s.Close()
				return n, err
			}
			log.Printf("failed to write to rtpWriter: %T: %v\n", err, err)
		}
		return n, err
	}))

	s.rtcpReader = i.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	pipeline, err := gstsrc.NewPipeline(s.codec, s.src, s)
	if err != nil {
		return err
	}
	s.pipeline = pipeline

	gstsrc.HandleSrcEOS(func() {
		log.Println("EOS")
		err := i.Close()
		if err != nil {
			log.Println(err)
		}
		err = s.rtpConn.Close()
		if err != nil {
			log.Println(err)
		}
		err = s.rtcpConn.Close()
		if err != nil {
			log.Println(err)
		}
		pipeline.Destroy()
		close(s.closed)
	})

	s.pipeline.SetSSRC(uint(s.streamInfo.SSRC))
	s.pipeline.Start()

	go gstsrc.StartMainLoop()

	return nil
}

func (s *Sender) NotifyDone(c chan<- struct{}) {
	go func() {
		<-s.closed
		c <- struct{}{}
	}()
}

func (s *Sender) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *Sender) Close() error {
	if s.isClosed() {
		return nil
	}
	s.pipeline.Stop()
	select {
	case <-time.After(2 * time.Second):
		return fmt.Errorf("sender close timed out after 2 seconds")
	case <-s.closed:
	}
	return nil
}
