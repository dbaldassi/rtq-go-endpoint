package utils

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

type bitrateConfig struct {
	mutex       sync.Mutex
	currentStep int
	steps       []int
	lastChange  time.Time
}

func (b *bitrateConfig) increase() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	//if time.Since(b.lastChange).Seconds() < 1 {
	//	return
	//}
	if b.currentStep >= len(b.steps)-1 {
		return
	}
	b.currentStep++
	b.lastChange = time.Now()
}

func (b *bitrateConfig) decrease() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	//if time.Since(b.lastChange).Milliseconds() < 500 {
	//	return
	//}
	if b.currentStep <= 0 {
		return
	}
	b.currentStep--
}

func (b *bitrateConfig) target() int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.steps[b.currentStep]
}

type localStream struct {
	queue         chan *rtp.Packet
	targetBitrate bitrateConfig
}

type SenderInterceptorFactory struct {
	interceptor *SenderInterceptor
}

func NewSenderInterceptor() *SenderInterceptorFactory {
	return &SenderInterceptorFactory{
		interceptor: &SenderInterceptor{
			NoOp:      interceptor.NoOp{},
			close:     make(chan struct{}),
			wg:        sync.WaitGroup{},
			log:       logging.NewDefaultLoggerFactory().NewLogger("naive_adaptive_sender"),
			streamsMu: sync.Mutex{},
			streams:   map[uint32]*localStream{},
		},
	}
}

func (f *SenderInterceptorFactory) NewInterceptor(is string) (interceptor.Interceptor, error) {
	return f.interceptor, nil
}

func (f *SenderInterceptorFactory) GetTargetBitrate(_ string, ssrc uint32) (int, error) {
	return f.interceptor.GetTargetBitrate(ssrc)
}

func (f *SenderInterceptorFactory) GetStatistics(string) string {
	return f.interceptor.GetStatistics()
}

type SenderInterceptor struct {
	interceptor.NoOp

	close chan struct{}
	wg    sync.WaitGroup

	log       logging.LeveledLogger // TODO: Replace logger?
	streamsMu sync.Mutex
	streams   map[uint32]*localStream
}

func (s *SenderInterceptor) initialTargetBitrate() bitrateConfig {
	return bitrateConfig{
		currentStep: 0,
		steps:       []int{256_000, 512_000, 768_000, 1_024_000, 1_280_000},
	}
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once for per LocalStream. The returned method
// will be called once per rtp packet.
func (s *SenderInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {

	ls := localStream{
		queue:         make(chan *rtp.Packet, 1_000_000),
		targetBitrate: s.initialTargetBitrate(),
	}
	s.streamsMu.Lock()
	s.streams[info.SSRC] = &ls
	s.streamsMu.Unlock()

	s.wg.Add(1)

	go s.loop(writer, info.SSRC)

	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, _ interceptor.Attributes) (int, error) {
		pkt := &rtp.Packet{Header: *header, Payload: payload}
		ls.queue <- pkt
		return pkt.MarshalSize(), nil
	})
}

// UnbindLocalStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (s *SenderInterceptor) UnbindLocalStream(info *interceptor.StreamInfo) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	if _, ok := s.streams[info.SSRC]; !ok {
		return
	}
	close(s.streams[info.SSRC].queue)
	delete(s.streams, info.SSRC)
}

func (s *SenderInterceptor) Close() error {
	defer s.wg.Wait()
	if !s.isClosed() {
		close(s.close)
	}
	return nil
}

func (s *SenderInterceptor) isClosed() bool {
	select {
	case <-s.close:
		return true
	default:
		return false
	}
}

func (s *SenderInterceptor) loop(writer interceptor.RTPWriter, ssrc uint32) {
	defer s.wg.Done()

	s.streamsMu.Lock()
	stream := s.streams[ssrc]
	s.streamsMu.Unlock()

	lastQueueSize := 0

	queueGrowing := 0
	queueShrinking := 0

	var lastDecrease time.Time
	var lastIncrease time.Time

	for {
		select {
		case packet := <-stream.queue:
			nextQueueSize := len(stream.queue)
			if _, err := writer.Write(&packet.Header, packet.Payload, interceptor.Attributes{}); err != nil {
				s.log.Warnf("failed sending RTP packet: %v", err)
			}

			if nextQueueSize == 0 && time.Since(lastIncrease) > 500*time.Millisecond {
				stream.targetBitrate.increase()
				queueGrowing = 0
				queueShrinking = 0
				lastIncrease = time.Now()
			}

			if nextQueueSize > lastQueueSize || nextQueueSize > 200 {
				queueGrowing++
				if queueGrowing > 50 && time.Since(lastDecrease) > 50*time.Millisecond {
					stream.targetBitrate.decrease()
					lastDecrease = time.Now()
					queueGrowing = 0
				}
			}

			if nextQueueSize < 100 && nextQueueSize < lastQueueSize {
				queueShrinking++
				if queueShrinking > 100 && time.Since(lastIncrease) > 500*time.Millisecond {
					stream.targetBitrate.increase()
					queueShrinking = 0
				}
			}
			lastQueueSize = nextQueueSize

		case <-s.close:
			return
		}
	}
}

func (s *SenderInterceptor) GetTargetBitrate(ssrc uint32) (int, error) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	ls, ok := s.streams[ssrc]
	if !ok {
		return 0, fmt.Errorf("unknown SSRC")
	}
	return ls.targetBitrate.target(), nil
}

func (s *SenderInterceptor) GetStatistics() string {
	result := ""
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	for _, stream := range s.streams {
		result += fmt.Sprintf("%v, ", len(stream.queue))
	}
	return result
}
