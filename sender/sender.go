package rtc

import (
	"fmt"
	"io"
	"time"

	gst "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
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

type Sender struct {
	codec string
	src   string

	pipeline *gst.Pipeline

	mtu      int
	rtpConn  RTPWriteCloser
	rtcpConn io.Reader

	streamInfo   *interceptor.StreamInfo
	interceptors interceptor.Registry
	rtpWriter    interceptor.RTPWriter
	rtcpReader   interceptor.RTCPReader

	closed chan struct{}
}

type Option func(*Sender) error

func NewSender(t RTPWriteCloser, opts ...Option) (*Sender, error) {
	s := &Sender{
		codec: "h264",
		src:   "videotestsrc",
		streamInfo: &interceptor.StreamInfo{
			SSRC: 0,
		},
		interceptors: interceptor.Registry{},
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
	var pkt rtp.Packet
	err = pkt.Unmarshal(p)
	if err != nil {
		return 0, err
	}
	n, err = s.rtpWriter.Write(&pkt.Header, p[pkt.Header.MarshalSize():], nil)
	return
}

func (s *Sender) AcceptFeedback() error {
	if s.rtcpConn == nil {
		return fmt.Errorf("cannot read rtcp with nil reader")
	}
	for buffer := make([]byte, s.mtu); ; {
		n, err := s.rtcpConn.Read(buffer)
		if err != nil {
			return err
		}
		if _, _, err := s.rtcpReader.Read(buffer[:n], interceptor.Attributes{}); err != nil {
			return err
		}
	}
}

func (s *Sender) Start() error {
	i := s.interceptors.Build()

	s.rtpWriter = i.BindLocalStream(s.streamInfo, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		return s.rtpConn.WriteRTP(header, payload)
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
		err := i.Close()
		if err != nil {
		}
		err = s.rtpConn.Close()
		if err != nil {
		}
		pipeline.Destroy()
		close(s.closed)
	})

	s.pipeline.SetSSRC(uint(s.streamInfo.SSRC))
	s.pipeline.Start()

	go gstsrc.StartMainLoop()

	return nil
}

func (s *Sender) Close() error {
	s.pipeline.Stop()
	select {
	case <-time.After(10 * time.Second):
		return fmt.Errorf("sender close timed out after 10 seconds")
	case <-s.closed:
	}
	return nil
}
