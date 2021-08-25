package rtc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	gstsink "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-sink"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/scream"
	"github.com/pion/rtcp"
)

type RTCPWriteCloser interface {
	WriteRTCP(pkts []rtcp.Packet) (int, error)
	Close() error
}

type RTPReadCloser interface {
	io.Reader
	io.Closer
}

type Receiver struct {
	codec string
	dst   string
	mtu   int

	rtcpConn RTCPWriteCloser
	rtpConn  RTPReadCloser

	streamInfo   *interceptor.StreamInfo
	interceptors interceptor.Registry
	rtpReader    interceptor.RTCPReader
	rtcpWriter   interceptor.RTCPWriter

	pipeline *gstsink.Pipeline

	closed chan struct{}
}

type ReceiverOption func(*Receiver) error

func ReceiverCodec(codec string) ReceiverOption {
	return func(r *Receiver) error {
		r.codec = codec
		return nil
	}
}

func NewReceiver(r RTPReadCloser, w RTCPWriteCloser, opts ...ReceiverOption) (*Receiver, error) {
	recv := &Receiver{
		codec:    "h264",
		dst:      "autovideosink",
		mtu:      1400,
		rtpConn:  r,
		rtcpConn: w,
		streamInfo: &interceptor.StreamInfo{
			SSRC: 0,
		},
		interceptors: interceptor.Registry{},
		closed:       make(chan struct{}),
	}
	for _, opt := range opts {
		err := opt(recv)
		if err != nil {
			return nil, err
		}
	}
	return recv, nil
}

func (r *Receiver) ConfigureSCReAMInterceptor() error {
	cc, err := scream.NewReceiverInterceptor()
	if err != nil {
		return err
	}
	r.streamInfo.RTCPFeedback = append(r.streamInfo.RTCPFeedback, interceptor.RTCPFeedback{
		Type:      "ack",
		Parameter: "ccfb",
	})
	r.interceptors.Add(cc)
	return nil
}

func (r *Receiver) ConfigureRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut io.WriteCloser) {
	i := utils.NewRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut)
	r.interceptors.Add(i)
}

func (r *Receiver) Receive() error {
	i := r.interceptors.Build()

	pipeline, err := gstsink.NewPipeline(r.codec, r.dst)
	if err != nil {
		return err
	}
	r.pipeline = pipeline
	gstsink.HandleSinkEOS(func() {
		err := i.Close()
		if err != nil {
			log.Println(err)
		}
		err = r.rtpConn.Close()
		if err != nil {
			log.Println(err)
		}
		err = r.rtcpConn.Close()
		if err != nil {
			log.Println(err)
		}
		r.pipeline.Destroy()
		close(r.closed)
	})

	r.rtpReader = i.BindRemoteStream(r.streamInfo, interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		r.pipeline.Push(in)
		return len(in), nil, nil
	}))

	r.rtcpWriter = i.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
		return r.rtcpConn.WriteRTCP(pkts)
	}))

	go func() {
		for buffer := make([]byte, r.mtu); ; {
			n, err := r.rtpConn.Read(buffer)
			if err != nil {
				if err == io.EOF || errors.Is(err, net.ErrClosed) {
					r.Close()
					return
				}
				log.Printf("failed to read from rtpConn: %v\n", err)
				return
			}
			if res := bytes.Compare(buffer[:n], []byte("eos")); res == 0 {
				r.Close()
				break
			}
			if _, _, err := r.rtpReader.Read(buffer[:n], nil); err != nil {
				log.Printf("rtpReader failed to read received buffer: %v\n", err)
				return
			}
		}
	}()

	r.pipeline.Start()
	go gstsink.StartMainLoop()

	return nil
}

func (r *Receiver) NotifyDone(c chan<- struct{}) {
	go func() {
		<-r.closed
		c <- struct{}{}
	}()
}

func (r *Receiver) isClosed() bool {
	select {
	case <-r.closed:
		return true
	default:
		return false
	}
}

func (r *Receiver) Close() error {
	r.pipeline.Stop()
	select {
	case <-time.After(2 * time.Second):
		return fmt.Errorf("sender close timed out after 2 seconds")
	case <-r.closed:
	}
	return nil
}
