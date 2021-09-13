package rtc

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"time"

	gstsink "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-sink"
	"github.com/mengelbart/rtq-go-endpoint/internal/scream"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

type RTCPWriter interface {
	WriteRTCP(pkts []rtcp.Packet) (int, error)
}

type Receiver struct {
	codec string
	dst   string
	mtu   int

	rtcpConn RTCPWriter
	rtpConn  io.Reader

	streamInfo *interceptor.StreamInfo
	ir         interceptor.Registry
	i          interceptor.Interceptor

	rtpReader  interceptor.RTCPReader
	rtcpWriter interceptor.RTCPWriter

	pipeline *gstsink.Pipeline

	packet  chan []byte
	closeC  chan struct{}
	notifyC chan<- struct{}
}

type ReceiverOption func(*Receiver) error

func ReceiverCodec(codec string) ReceiverOption {
	return func(r *Receiver) error {
		r.codec = codec
		return nil
	}
}

func ReceiverDst(dst string) ReceiverOption {
	return func(r *Receiver) error {
		r.dst = dst
		return nil
	}
}

func NewReceiver(r io.Reader, w RTCPWriter, opts ...ReceiverOption) (*Receiver, error) {
	recv := &Receiver{
		codec:    "h264",
		dst:      "autovideosink",
		mtu:      1400,
		rtpConn:  r,
		rtcpConn: w,
		streamInfo: &interceptor.StreamInfo{
			SSRC: 0,
		},
		ir:     interceptor.Registry{},
		packet: make(chan []byte, 1000),
		closeC: make(chan struct{}),
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
	r.ir.Add(cc)
	return nil
}

func (r *Receiver) ConfigureRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut io.WriteCloser) {
	i := utils.NewRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut)
	r.ir.Add(i)
}

func (r *Receiver) Receive() error {
	i := r.ir.Build()
	r.i = i

	pipeline, err := gstsink.NewPipeline(r.codec, r.dst)
	if err != nil {
		return err
	}
	r.pipeline = pipeline

	eosC := make(chan struct{})
	gstsink.HandleSinkEOS(func() {
		close(eosC)
	})

	r.rtpReader = r.i.BindRemoteStream(r.streamInfo, interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		r.pipeline.Push(in)
		return len(in), nil, nil
	}))

	r.rtcpWriter = r.i.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
		return r.rtcpConn.WriteRTCP(pkts)
	}))

	connErrC := make(chan error)
	go func() {
		for buffer := make([]byte, r.mtu); ; {
			n, err := r.rtpConn.Read(buffer)
			if err != nil {
				connErrC <- err
				return
			}
			if res := bytes.Compare(buffer[:n], []byte("eos")); res == 0 {
				connErrC <- fmt.Errorf("connection got EOS")
				return
			}
			if _, _, err := r.rtpReader.Read(buffer[:n], nil); err != nil {
				connErrC <- fmt.Errorf("rtpReader failed to read received buffer: %w", err)
				return
			}
		}
	}()

	r.pipeline.Start()
	go gstsink.StartMainLoop()

	go func() {
		select {
		case err := <-connErrC:
			log.Printf("got error from connection reader: %v\n", err)
			r.pipeline.Stop()

		case <-r.closeC:
			r.pipeline.Stop()
		}
		r.i.Close()
		select {
		case <-eosC:
		case <-time.After(3 * time.Second):
			log.Printf("timeout")
		}
		if r.notifyC != nil {
			r.notifyC <- struct{}{}
		}
	}()

	return nil
}

func (r *Receiver) NotifyDone(c chan<- struct{}) {
	r.notifyC = c
}

func (r *Receiver) Close() error {
	close(r.closeC)
	return nil
}
