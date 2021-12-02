module github.com/mengelbart/rtq-go-endpoint

go 1.16

require (
	github.com/lucas-clemente/quic-go v0.22.1
	github.com/mengelbart/rtq-go v0.1.1-0.20210913160810-c84302426051
	github.com/mengelbart/scream-go v0.3.0
	github.com/mengelbart/syncodec v0.0.0-20211121123228-d94cb52b9e9e
	github.com/pion/interceptor v0.1.3-0.20211202180006-e2cf127b8ed7
	github.com/pion/logging v0.2.2
	github.com/pion/rtcp v1.2.9
	github.com/pion/rtp v1.7.4
	github.com/pion/webrtc/v3 v3.1.11
)

replace github.com/lucas-clemente/quic-go v0.22.1 => github.com/mengelbart/quic-go v0.7.1-0.20210909103716-3e25d1c2cf91
