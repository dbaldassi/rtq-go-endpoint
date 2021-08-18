package udp

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"os/signal"

	gstsink "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-sink"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/scream"
	"github.com/pion/rtcp"
)

const (
	mtu = 1200
)

type Receiver struct {
	Addr  string
	Codec string
	CC    string

	RTCPInLog  io.WriteCloser
	RTCPOutLog io.WriteCloser
	RTPInLog   io.WriteCloser
	RTPOutLog  io.WriteCloser
}

type ReceiverOption func(*Receiver) error

func ReceiverCodec(codec string) ReceiverOption {
	return func(r *Receiver) error {
		r.Codec = codec
		return nil
	}
}

func ReceiverCongestionControl(cc string) ReceiverOption {
	return func(r *Receiver) error {
		r.CC = cc
		return nil
	}
}

func ReceiverRTCPInLogWriter(w io.WriteCloser) ReceiverOption {
	return func(r *Receiver) error {
		r.RTCPInLog = w
		return nil
	}
}

func ReceiverRTCPOutLogWriter(w io.WriteCloser) ReceiverOption {
	return func(r *Receiver) error {
		r.RTCPOutLog = w
		return nil
	}
}

func ReceiverRTPInLogWriter(w io.WriteCloser) ReceiverOption {
	return func(r *Receiver) error {
		r.RTPInLog = w
		return nil
	}
}

func ReceiverRTPOutLogWriter(w io.WriteCloser) ReceiverOption {
	return func(r *Receiver) error {
		r.RTPOutLog = w
		return nil
	}
}

func NewReceiver(addr string, opts ...ReceiverOption) (*Receiver, error) {
	r := &Receiver{
		Addr:       addr,
		Codec:      "h264",
		CC:         "no-cc",
		RTCPInLog:  os.Stdout,
		RTCPOutLog: os.Stdout,
		RTPInLog:   os.Stdout,
		RTPOutLog:  os.Stdout,
	}
	for _, opt := range opts {
		err := opt(r)
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Receiver) Receive(dst string) error {
	addr, err := net.ResolveUDPAddr("udp4", r.Addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}

	pipeline, err := gstsink.NewPipeline(r.Codec, dst)
	if err != nil {
		return err
	}
	log.Printf("created pipeline: '%v'\n", pipeline.String())

	rtpLog := utils.NewRTPLogInterceptor(r.RTCPInLog, r.RTCPOutLog, r.RTPInLog, r.RTPOutLog)
	interceptors := []interceptor.Interceptor{rtpLog}
	var rtcpfb []interceptor.RTCPFeedback

	switch r.CC {
	case SCReAM:
		feedback, err := scream.NewReceiverInterceptor()
		if err != nil {
			return err
		}
		rtcpfb = []interceptor.RTCPFeedback{
			{Type: "ack", Parameter: "ccfb"},
		}
		interceptors = append(interceptors, feedback)

	default:
		rtcpfb = []interceptor.RTCPFeedback{}
	}

	chain := interceptor.NewChain(interceptors)

	streamReader := chain.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:         RTPSSRC,
		RTCPFeedback: rtcpfb,
	}, interceptor.RTPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		pipeline.Push(in)
		return len(in), nil, nil
	}))

	destroyed := make(chan struct{}, 1)
	gstsink.HandleSinkEOS(func() {
		pipeline.Destroy()
		destroyed <- struct{}{}
	})
	pipeline.Start()

	done := make(chan struct{}, 1)
	errChan := make(chan error, 1)
	go func() {
		for rtcpBound, buffer := false, make([]byte, mtu); ; {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				if err == io.EOF {
					close(done)
					break
				}
				errChan <- err
			}
			if res := bytes.Compare(buffer[:n], []byte("eos")); res == 0 {
				close(done)
				break
			}
			if _, _, err := streamReader.Read(buffer[:n], nil); err != nil {
				errChan <- err
			}
			if !rtcpBound {
				chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
					buf, err := rtcp.Marshal(pkts)
					if err != nil {
						return 0, err
					}
					return conn.WriteTo(buf, addr)
				}))
				rtcpBound = true
			}
		}
	}()
	go gstsink.StartMainLoop()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	select {
	case err1 := <-errChan:
		err = err1
	case <-done:
	case <-signals:
		log.Printf("got interrupt signal")
	}

	log.Println("stopping pipeline")
	pipeline.Stop()
	<-destroyed
	log.Println("destroyed pipeline, exiting")
	return err
}
