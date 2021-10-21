package rtc

import (
	"log"
	"net"
	"sort"
	"time"

	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	screamcgo "github.com/mengelbart/scream-go"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

func getNTPT0() float64 {
	now := time.Now()
	secs := now.Unix()
	usecs := now.UnixMicro() - secs*1e6
	return (float64(secs) + float64(usecs)*1e-6) - 1e-3
}

func getTimeBetweenNTP(t0 float64, tx time.Time) uint64 {
	secs := tx.Unix()
	usecs := tx.UnixMicro() - secs*1e6
	tt := (float64(secs) + float64(usecs)*1e-6) - t0
	ntp64 := uint64(tt * 65536.0)
	ntp := 0xFFFFFFFF & ntp64
	return ntp
}

type Metricer interface {
	Metrics() utils.RTTStats
}

type fbInferer struct {
	rtpConn  AckingRTPWriter
	rx       *screamcgo.Rx
	received chan []byte
	acked    chan ackedPkt
	t0       float64
	m        Metricer
}

func newFBInferer(w AckingRTPWriter, rx *screamcgo.Rx, received chan []byte, m Metricer) *fbInferer {
	return &fbInferer{
		rtpConn:  w,
		rx:       rx,
		received: received,
		acked:    make(chan ackedPkt, 1000),
		t0:       getNTPT0(),
		m:        m,
	}
}

type ackedPkt struct {
	sentTS time.Time
	ssrc   uint32
	size   int
	seqNr  uint16
}

func (f *fbInferer) ntpTime(t time.Time) uint64 {
	return getTimeBetweenNTP(f.t0, t)
}

func (f *fbInferer) buffer(cancel chan struct{}) {
	t := time.NewTicker(10 * time.Millisecond)
	var buf []ackedPkt
	for {
		select {
		case pkt := <-f.acked:
			buf = append(buf, pkt)

		case <-t.C:
			if len(buf) == 0 {
				continue
			}
			sort.Slice(buf, func(i, j int) bool {
				return buf[i].seqNr < buf[j].seqNr
			})

			metrics := f.m.Metrics()
			var lastTS uint64
			for _, pkt := range buf {
				sent := f.ntpTime(pkt.sentTS)
				rttNTP := metrics.SmoothedRTT.Seconds() * 65536
				lastTS = sent + uint64(rttNTP)/2

				//lastTS2 := f.ntpTime(pkt.sentTS.Add(metrics.MinRTT / 2))
				//fmt.Printf("t=%v, t2=%v, diff=%v\n", lastTS, lastTS2, lastTS-lastTS2)
				f.rx.Receive(lastTS, pkt.ssrc, pkt.size, pkt.seqNr, 0)
			}
			buf = []ackedPkt{}

			if ok, fb := f.rx.CreateStandardizedFeedback(lastTS, true); ok {
				f.received <- fb
			}

		case <-cancel:
			return
		}
	}
}

func (f *fbInferer) rtpWriterFunc(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
	size := header.MarshalSize() + len(payload)
	t := time.Now()
	n, err := f.rtpConn.WriteRTPNotify(header, payload, func(r bool) {
		if !r {
			return // ignore lost packets
		}
		go func() {
			f.acked <- ackedPkt{
				sentTS: t,
				ssrc:   header.SSRC,
				size:   size,
				seqNr:  header.SequenceNumber,
			}
		}()
	})

	if err != nil {
		if netErr, ok := err.(net.Error); ok && !netErr.Temporary() || err.Error() == "Application error 0x0: eos" {
			return n, err
		}
		log.Printf("failed to write to rtpWriter: %T: %v\n", err, err)
	}

	return n, nil
}
