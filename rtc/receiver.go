package rtc

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"time"

	gstsink "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-sink"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/packetdump"
	"github.com/pion/interceptor/pkg/scream"
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

	rtpReader interceptor.RTCPReader

	pipeline *gstsink.Pipeline

	packet chan []byte
	closeC chan struct{}
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
		mtu:      1200,
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

func (r *Receiver) ConfigureRTPLogInterceptor(rtcpWriter, rtpWriter io.Writer, rtpFormat packetdump.RTPFormatCallback, rtcpFormat packetdump.RTCPFormatCallback) error {
	rtcpDumperInterceptor, err := packetdump.NewSenderInterceptor(
		packetdump.RTCPFormatter(rtcpFormat),
		packetdump.RTCPWriter(rtcpWriter),
	)
	if err != nil {
		return err
	}
	rtpDumperInterceptor, err := packetdump.NewReceiverInterceptor(
		packetdump.RTPFormatter(rtpFormat),
		packetdump.RTPWriter(rtpWriter),
	)
	if err != nil {
		return err
	}

	r.ir.Add(rtpDumperInterceptor)
	r.ir.Add(rtcpDumperInterceptor)
	return nil
}

func (r *Receiver) Receive() error {
	i, err := r.ir.Build("")
	if err != nil {
		return err
	}
	r.i = i

	if r.codec != "synthetic" {
		pipeline, err := gstsink.NewPipeline(r.codec, r.dst)
		if err != nil {
			return err
		}
		r.pipeline = pipeline
	}

	eosC := make(chan struct{})
	gstsink.HandleSinkEOS(func() {
		close(eosC)
	})

	r.rtpReader = r.i.BindRemoteStream(r.streamInfo, interceptor.RTCPReaderFunc(func(in []byte, _ interceptor.Attributes) (int, interceptor.Attributes, error) {
		if r.codec != "synthetic" {
			r.pipeline.Push(in)
		}
		return len(in), nil, nil
	}))

	_ = r.i.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
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

	if r.codec != "synthetic" {
		r.pipeline.Start()
		go gstsink.StartMainLoop()
	}

	select {
	case err := <-connErrC:
		log.Printf("got error from connection reader: %v\n", err)
		if r.codec != "synthetic" {
			r.pipeline.Stop()
		}

	case <-r.closeC:
		if r.codec != "synthetic" {
			r.pipeline.Stop()
		}
	}
	r.i.Close()
	select {
	case <-eosC:
	case <-time.After(3 * time.Second):
		log.Printf("timeout")
	}

	return nil
}

func (r *Receiver) Close() error {
	close(r.closeC)
	return nil
}
