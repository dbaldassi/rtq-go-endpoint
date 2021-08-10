package udp

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"

	gstsrc "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/scream"
	"github.com/pion/rtp"
)

type Sender struct {
	Addr  string
	Codec string
	CC    string
}

func (s *Sender) Send(src string) error {
	addr, err := net.ResolveUDPAddr("udp4", s.Addr)
	if err != nil {
		return err
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return err
	}

	var chain *interceptor.Chain
	var rtcpfb []interceptor.RTCPFeedback

	var cc *scream.SenderInterceptor

	switch s.CC {
	case SCReAM:
		cc, err = scream.NewSenderInterceptor()
		if err != nil {
			return err
		}
		rtcpfb = []interceptor.RTCPFeedback{
			{Type: "ack", Parameter: "ccfb"},
		}
		chain = interceptor.NewChain([]interceptor.Interceptor{cc})

	default:
		rtcpfb = []interceptor.RTCPFeedback{}
		chain = interceptor.NewChain([]interceptor.Interceptor{})
	}

	streamWriter := chain.BindLocalStream(&interceptor.StreamInfo{
		SSRC:         RTPSSRC,
		RTCPFeedback: rtcpfb,
	}, interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		headerBuf, err := header.Marshal()
		if err != nil {
			return 0, err
		}

		return conn.Write(append(headerBuf, payload...))
	}))
	rtcpReader := chain.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	writer := &gstWriter{
		conn:       conn,
		rtpWriter:  streamWriter,
		rtcpReader: rtcpReader,
		cc:         cc,
	}
	go func() {
		err := writer.acceptFeedback()
		if err != nil && err != io.EOF && !errors.Is(err, net.ErrClosed) {
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
			log.Printf("failed to close udp writer: %s\n", err.Error())
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
	conn          *net.UDPConn
	pipeline      *gstsrc.Pipeline
	rtcpReader    interceptor.RTCPReader
	rtpWriter     interceptor.RTPWriter
	cc            *scream.SenderInterceptor
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
	for buffer := make([]byte, mtu); ; {
		n, err := g.conn.Read(buffer)
		if err != nil {
			return err
		}
		if _, _, err := g.rtcpReader.Read(buffer[:n], interceptor.Attributes{}); err != nil {
			return err
		}
		if g.cc != nil {
			bitrate, err := g.cc.GetTargetBitrate(RTPSSRC)
			if err != nil {
				return err
			}
			if bitrate != g.targetBitrate && bitrate > 0 {
				g.targetBitrate = bitrate
				log.Printf("new target bitrate: %v\n", bitrate)
				g.pipeline.SetBitRate(uint(bitrate / 1000)) // Gstreamer expects kbit/s
			}
		}
	}
}

func (g *gstWriter) Close() error {
	_, err := g.conn.Write([]byte("eos"))
	if err != nil {
		return err
	}
	return g.conn.Close()
}
