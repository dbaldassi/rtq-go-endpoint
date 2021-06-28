module github.com/mengelbart/rtq-go-endpoint

go 1.16

require (
	github.com/lucas-clemente/quic-go v0.20.1
	github.com/mengelbart/rtq-go v0.1.1-0.20210628140847-d2823c56266c
	github.com/pion/interceptor v0.0.13-0.20210425121424-8bb0c0609bbd
	github.com/pion/rtp v1.6.2
)

replace github.com/mengelbart/rtq-go => ../rtq-go
