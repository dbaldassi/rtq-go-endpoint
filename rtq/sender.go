package rtq

import (
	"crypto/tls"
	"io"
	"log"
	"os"
	"os/signal"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtq-go"
	gstsrc "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

type Sender struct {
	Addr       string
	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Codec      string
}

func (s *Sender) Send(src string) error {
	quicSession, err := quic.DialAddr(s.Addr, s.TLSConfig, s.QUICConfig)
	if err != nil {
		return err
	}
	rtqSession, err := rtq.NewSession(quicSession)
	if err != nil {
		return err
	}

	rtpFlow, err := rtqSession.OpenWriteFlow(0)
	if err != nil {
		return err
	}

	chain := interceptor.NewChain([]interceptor.Interceptor{})
	streamWriter := chain.BindLocalStream(&interceptor.StreamInfo{
		SSRC:         RTPSSRC,
		RTCPFeedback: []interceptor.RTCPFeedback{},
	}, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		return rtpFlow.WriteRTP(header, payload)
	}))
	rtcpReader := chain.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	writer := &gstWriter{
		rtqSession: rtqSession,
		rtpWriter:  streamWriter,
		rtcpReader: rtcpReader,
	}
	go func() {
		err := writer.acceptFeedback()
		if err != nil && err != io.EOF {
			// TODO: Handle error properly
			panic(err)
		}
	}()

	pipeline, err := gstsrc.NewPipeline(s.Codec, src, writer)
	if err != nil {
		return err
	}
	log.Printf("created pipeline: '%v'\n", pipeline.String())
	writer.pipeline = pipeline
	pipeline.SetSSRC(0)
	pipeline.Start()

	go gstsrc.StartMainLoop()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	done := make(chan struct{}, 1)
	destroyed := make(chan struct{}, 1)
	gstsrc.HandleSinkEOS(func() {
		log.Println("got EOS, stopping pipeline")
		err := writer.Close()
		if err != nil {
			log.Printf("failed to close rtq session: %s\n", err.Error())
		}
		err = chain.Close()
		if err != nil {
			log.Printf("failed to close interceptor chain: %s\n", err.Error())
		}
		close(done)
		pipeline.Destroy()
		destroyed <- struct{}{}
	})

	select {
	case <-signals:
		log.Printf("got interrupt signal, stopping pipeline")
		pipeline.Stop()
	case <-done:
	}

	<-destroyed
	log.Println("destroyed pipeline, exiting")

	return err
}

type gstWriter struct {
	targetBitrate int64
	rtqSession    *rtq.Session
	pipeline      *gstsrc.Pipeline
	rtcpReader    interceptor.RTCPReader
	rtpWriter     interceptor.RTPWriter
}

func (g *gstWriter) Write(p []byte) (n int, err error) {
	var pkt rtp.Packet
	err = pkt.Unmarshal(p)
	if err != nil {
		return 0, err
	}
	n, err = g.rtpWriter.Write(&pkt.Header, p[pkt.Header.MarshalSize():], nil)
	if err != nil {
		log.Printf("failed to write paket: %v, stopping pipeline\n", err.Error())
		g.pipeline.Stop()
	}
	return
}

func (g *gstWriter) acceptFeedback() error {
	rtcpFlow, err := g.rtqSession.AcceptFlow(RTCPSSRC)
	if err != nil {
		return err
	}
	for buffer := make([]byte, mtu); ; {
		n, err := rtcpFlow.Read(buffer)
		if err != nil {
			return err
		}
		if _, _, err := g.rtcpReader.Read(buffer[:n], interceptor.Attributes{}); err != nil {
			return err
		}
	}
}

func (g *gstWriter) Close() error {
	return g.rtqSession.Close()
}
