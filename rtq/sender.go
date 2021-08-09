package rtq

import (
	"crypto/tls"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/lucas-clemente/quic-go"
	"github.com/mengelbart/rtq-go"
	gstsrc "github.com/mengelbart/rtq-go-endpoint/internal/gstreamer-src"
	cscream "github.com/mengelbart/scream-go"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/scream"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type Sender struct {
	Addr       string
	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Codec      string
	CC         string
}

func ntpTime(t time.Time) uint64 {
	// seconds since 1st January 1900
	s := (float64(t.UnixNano()) / 1000000000) + 2208988800

	// higher 32 bits are the integer part, lower 32 bits are the fractional part
	integerPart := uint32(s)
	fractionalPart := uint32((s - float64(integerPart)) * 0xFFFFFFFF)
	return uint64(integerPart)<<32 | uint64(fractionalPart)
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

	var chain *interceptor.Chain
	var rtcpfb []interceptor.RTCPFeedback

	var cc *scream.SenderInterceptor
	type ackedPkt struct {
		ssrc   uint32
		seqNr  uint16
		length int
	}
	received := make(chan ackedPkt, 1000)

	var tx *cscream.Tx
	switch s.CC {
	case SCReAM:
		tx = cscream.NewTx()
		cc, err = scream.NewSenderInterceptor(scream.Tx(tx))
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
		defer func(t time.Time) {
			log.Printf("write took %v\n", time.Now().Sub(t))
		}(time.Now())
		n := header.MarshalSize() + len(payload)
		return rtpFlow.WriteRTPNotify(header, payload, func(r bool) {
			go func() {
				received <- ackedPkt{
					ssrc:   header.SSRC,
					seqNr:  header.SequenceNumber,
					length: n,
				}
			}()
		})
	}))
	rtcpReader := chain.BindRTCPReader(interceptor.RTCPReaderFunc(func(in []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		return len(in), nil, nil
	}))

	writer := &gstWriter{
		rtqSession: rtqSession,
		rtpWriter:  streamWriter,
		rtcpReader: rtcpReader,
		cc:         cc,
	}

	pipeline, err := gstsrc.NewPipeline(s.Codec, src, writer)
	if err != nil {
		return err
	}
	log.Printf("created pipeline: '%v'\n", pipeline.String())
	writer.pipeline = pipeline
	pipeline.SetSSRC(0)
	pipeline.Start()

	go gstsrc.StartMainLoop()

	done := make(chan struct{}, 1)

	recorder := cscream.NewRx(RTPSSRC)
	go func() {
		for {
			select {
			case pkt := <-received:
				recorder.Receive(ntpTime(time.Now()), pkt.ssrc, pkt.length, pkt.seqNr, 0)
			case <-done:
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		rtt := 2 * time.Millisecond
		lastBitrate := int64(0)
		for {
			select {
			case <-ticker.C:
				t := time.Now().Add(-rtt)
				if ok, feedback := recorder.CreateStandardizedFeedback(ntpTime(t), true); ok {
					fb := rtcp.RawPacket(feedback)
					tx.IncomingStandardizedFeedback(ntpTime(time.Now()), fb)
				}
				bitrate := tx.GetTargetBitrate(RTPSSRC)
				if bitrate != lastBitrate && bitrate > 0 {
					lastBitrate = bitrate
					log.Printf("new target bitrate: %v\n", bitrate)
					pipeline.SetBitRate(uint(bitrate / 1000)) // Gstreamer expects kbit/s
				}
			case <-done:
				return
			}
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

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
	cc            *scream.SenderInterceptor
}

func (g *gstWriter) Write(p []byte) (n int, err error) {
	var pkt rtp.Packet
	err = pkt.Unmarshal(p)
	if err != nil {
		return 0, err
	}
	n, err = g.rtpWriter.Write(&pkt.Header, p[pkt.Header.MarshalSize():], nil)
	//log.Printf("sent packet of size %v bytes\n", n)
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
	return g.rtqSession.Close()
}
