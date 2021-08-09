package rtq

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"os"
	"os/signal"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtq-go"
	gstsink "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-sink"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
)

const (
	mtu = 1200

	// RTPSSRC and RTCPSSRC should at some point be negotiated via some
	// signalling (e.g. SDP)
	RTPSSRC  = 0
	RTCPSSRC = 1
)

type Receiver struct {
	Addr       string
	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Codec      string
}

func (r *Receiver) Receive(dst string) error {
	listener, err := quic.ListenAddr(r.Addr, r.TLSConfig, r.QUICConfig)
	if err != nil {
		return err
	}
	quicSession, err := listener.Accept(context.Background())
	if err != nil {
		return err
	}

	rtqSession, err := rtq.NewSession(quicSession)
	if err != nil {
		return err
	}

	rtqFlow, err := rtqSession.AcceptFlow(0)
	if err != nil {
		return err
	}

	pipeline, err := gstsink.NewPipeline(r.Codec, dst)
	if err != nil {
		return err
	}
	log.Printf("created pipeline: '%v'\n", pipeline.String())

	chain := interceptor.NewChain([]interceptor.Interceptor{})
	streamReader := chain.BindRemoteStream(&interceptor.StreamInfo{
		SSRC:         RTPSSRC,
		RTCPFeedback: []interceptor.RTCPFeedback{},
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
			n, err := rtqFlow.Read(buffer)
			if err != nil {
				if err == io.EOF {
					close(done)
					break
				}
				errChan <- err
			}
			if _, _, err := streamReader.Read(buffer[:n], nil); err != nil {
				errChan <- err
			}
			if !rtcpBound {
				rtcpFlow, err := rtqSession.OpenWriteFlow(RTCPSSRC)
				if err != nil {
					errChan <- err
				}
				chain.BindRTCPWriter(interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
					buf, err := rtcp.Marshal(pkts)
					if err != nil {
						return 0, err
					}
					return rtcpFlow.Write(buf)
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
		err := rtqSession.Close()
		if err != nil {
			log.Printf("failed to close rtq session: %v\n", err.Error())
		}
	}

	log.Println("stopping pipeline")
	pipeline.Stop()
	<-destroyed
	log.Println("destroyed pipeline, exiting")
	return err
}
