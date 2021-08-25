package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type rtcpPacket struct {
	rtcp.Packet
	receiveTime time.Time
}

func (p *rtcpPacket) String() string {
	out := "RTCP"

	out += fmt.Sprintf("\t%T", p.Packet)

	return out
}

type rtpPacket struct {
	*rtp.Packet
	receiveTime time.Time
}

func (p *rtpPacket) String() string {
	out := "RTP"

	out += fmt.Sprintf("\t%d", p.receiveTime.UnixNano())
	out += fmt.Sprintf("\t%d", p.PayloadType)
	out += fmt.Sprintf("\t%x", p.SSRC)
	out += fmt.Sprintf("\t%d", p.SequenceNumber)
	out += fmt.Sprintf("\t%d", p.Timestamp)
	out += fmt.Sprintf("\t%v", boolToChar(p.Marker))
	out += fmt.Sprintf("\t%v", len(p.Payload))

	return out
}

type RTPLogInterceptor struct {
	interceptor.NoOp

	rtcpInStream  io.WriteCloser
	rtcpOutStream io.WriteCloser
	rtpInStream   io.WriteCloser
	rtpOutStream  io.WriteCloser

	rtcpIn  chan *rtcpPacket
	rtcpOut chan *rtcpPacket
	rtpIn   chan *rtpPacket
	rtpOut  chan *rtpPacket

	done   chan struct{}
	closed chan struct{}
}

func NewRTPLogInterceptor(rtcpIn, rtcpOut, rtpIn, rtpOut io.WriteCloser) *RTPLogInterceptor {
	i := &RTPLogInterceptor{
		rtcpInStream:  rtcpIn,
		rtcpOutStream: rtcpOut,
		rtpInStream:   rtpIn,
		rtpOutStream:  rtpOut,

		rtcpIn:  make(chan *rtcpPacket),
		rtcpOut: make(chan *rtcpPacket),
		rtpIn:   make(chan *rtpPacket),
		rtpOut:  make(chan *rtpPacket),

		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	go i.loop()
	return i
}

// BindRTCPReader lets you modify any incoming RTCP packets. It is called once per sender/receiver, however this might
// change in the future. The returned method will be called once per packet batch.
func (r *RTPLogInterceptor) BindRTCPReader(reader interceptor.RTCPReader) interceptor.RTCPReader {
	return interceptor.RTCPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		i, attr, err := reader.Read(b, a)
		if err != nil {
			return 0, nil, err
		}
		pkts, err := rtcp.Unmarshal(b[:i])
		if err != nil {
			return 0, nil, err
		}
		for _, pkt := range pkts {
			r.rtcpIn <- &rtcpPacket{
				Packet:      pkt,
				receiveTime: time.Now(),
			}
		}
		return i, attr, err
	})
}

// BindRTCPWriter lets you modify any outgoing RTCP packets. It is called once per PeerConnection. The returned method
// will be called once per packet batch.
func (r *RTPLogInterceptor) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
	return interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attributes interceptor.Attributes) (int, error) {
		for _, pkt := range pkts {
			r.rtcpOut <- &rtcpPacket{
				Packet:      pkt,
				receiveTime: time.Now(),
			}
		}
		return writer.Write(pkts, attributes)
	})
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once for per LocalStream. The returned method
// will be called once per rtp packet.
func (r *RTPLogInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		r.rtpOut <- &rtpPacket{
			Packet: &rtp.Packet{
				Header:  *header,
				Payload: payload,
			},
			receiveTime: time.Now(),
		}
		return writer.Write(header, payload, attributes)
	})
}

// BindRemoteStream lets you modify any incoming RTP packets. It is called once for per RemoteStream. The returned method
// will be called once per rtp packet.
func (r *RTPLogInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	return interceptor.RTPReaderFunc(func(bytes []byte, attributes interceptor.Attributes) (int, interceptor.Attributes, error) {
		ts := time.Now()
		i, attr, err := reader.Read(bytes, attributes)
		if err != nil {
			return 0, nil, err
		}
		pkt := rtp.Packet{}
		if err = pkt.Unmarshal(bytes[:i]); err != nil {
			return 0, nil, err
		}
		r.rtpIn <- &rtpPacket{
			Packet:      &pkt,
			receiveTime: ts,
		}
		return i, attr, nil
	})
}

func (r *RTPLogInterceptor) Close() error {
	if !r.isClosed() {
		close(r.done)
	}
	<-r.closed
	for _, c := range []io.Closer{
		r.rtcpInStream,
		r.rtcpOutStream,
		r.rtpInStream,
		r.rtpOutStream,
	} {
		if c != os.Stdout {
			err := c.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RTPLogInterceptor) isClosed() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *RTPLogInterceptor) loop() {
	defer func() {
		r.closed <- struct{}{}
	}()
	for {
		select {
		case p := <-r.rtcpIn:
			if _, err := fmt.Fprintf(r.rtcpInStream, "in:\t%s\n", p); err != nil {
				log.Printf("could not dump RTCP packet %v", err)
			}
		case p := <-r.rtcpOut:
			if _, err := fmt.Fprintf(r.rtcpOutStream, "out:\t%s\n", p); err != nil {
				log.Printf("could not dump RTCP packet %v", err)
			}
		case p := <-r.rtpIn:
			if _, err := fmt.Fprintf(r.rtpInStream, "in:\t%s\n", p); err != nil {
				log.Printf("could not dump RTP packet %v", err)
			}
		case p := <-r.rtpOut:
			if _, err := fmt.Fprintf(r.rtpOutStream, "out:\t%s\n", p); err != nil {
				log.Printf("could not dump RTP packet %v", err)
			}
		case <-r.done:
			return
		}
	}
}

func boolToChar(b bool) string {
	if !b {
		return "0"
	}
	return "1"
}
