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
)

const mtu = 1200

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

	destroyed := make(chan struct{}, 1)
	gstsink.HandleSinkEOS(func() {
		pipeline.Destroy()
		destroyed <- struct{}{}
	})
	pipeline.Start()

	done := make(chan struct{}, 1)
	errChan := make(chan error, 1)
	go func() {
		for buffer := make([]byte, mtu); ; {
			n, err := rtqFlow.Read(buffer)
			if err != nil {
				if err == io.EOF {
					close(done)
					break
				}
				errChan <- err
			}
			pipeline.Push(buffer[:n])
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