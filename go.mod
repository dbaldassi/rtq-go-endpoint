module github.com/mengelbart/rtq-go-endpoint

go 1.16

require (
	github.com/lucas-clemente/quic-go v0.22.0
	github.com/mengelbart/rtq-go v0.1.1-0.20210719164621-f9a7d03528a5
	github.com/mengelbart/scream-go v0.2.1
	github.com/pion/interceptor v0.0.13-0.20210425121424-8bb0c0609bbd
	github.com/pion/rtcp v1.2.6
	github.com/pion/rtp v1.6.2
)

replace github.com/mengelbart/rtq-go v0.1.1-0.20210719164621-f9a7d03528a5 => github.com/mengelbart/rtq-go v0.1.1-0.20210809141046-4810b3fdd52a

replace github.com/lucas-clemente/quic-go v0.22.0 => github.com/mengelbart/quic-go v0.7.1-0.20210809134509-defbb651803a

replace github.com/pion/interceptor v0.0.13-0.20210425121424-8bb0c0609bbd => ../../pion/interceptor
