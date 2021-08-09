package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/qlog"
	endpoint "github.com/mengelbart/rtq-go-endpoint"
	"github.com/mengelbart/rtq-go-endpoint/internal/utils"
	"github.com/mengelbart/rtq-go-endpoint/rtq"
	"github.com/mengelbart/rtq-go-endpoint/udp"
)

func main() {
	logFilename := os.Getenv("LOG_FILE")
	if logFilename != "" {
		logfile, err := os.Create(logFilename)
		if err != nil {
			fmt.Printf("Could not create log file: %s\n", err.Error())
			os.Exit(1)
		}
		defer logfile.Close()
		log.SetOutput(logfile)
	}

	codec := flag.String("codec", endpoint.H264, "Video Codec")
	transport := flag.String("transport", endpoint.RTQTransport, fmt.Sprintf("Transport to use, options: '%v', '%v'", endpoint.RTQTransport, endpoint.UDPTransport))
	cc := flag.String("cc", "", fmt.Sprintf("Congestion Controller to use, options: '%v', '%v'", "", rtq.SCReAM))
	addr := flag.String("addr", ":4242", "address to listen and receive video")
	flag.Parse()

	files := flag.Args()

	log.Printf("args: %v\n", files)
	dstStr := "autovideosink"
	if len(files) > 0 {
		dstStr = fmt.Sprintf("matroskamux ! filesink location=%v", files[0])
	}

	var r endpoint.Receiver
	var err error

	switch *transport {
	case endpoint.RTQTransport:
		r, err = setupRTQReceiver(*addr, *cc, *codec)
		if err != nil {
			log.Fatal(err)
		}
	case endpoint.UDPTransport:
		r = &udp.Receiver{
			Addr:  *addr,
			Codec: *codec,
			CC:    *cc,
		}
	default:
		log.Fatalf("unknown transport: %v\n", *transport)
	}

	err = r.Receive(dstStr)

	if err != nil {
		log.Fatal(err)
	}
}

func setupRTQReceiver(addr, cc, codec string) (*rtq.Receiver, error) {
	qlogWriter, err := utils.GetQLOGWriter()
	if err != nil {
		return nil, fmt.Errorf("could not get qlog writer: %w", err)
	}

	quicConf := &quic.Config{
		EnableDatagrams: true,
	}
	if qlogWriter != nil {
		quicConf.Tracer = qlog.NewTracer(qlogWriter)
	}
	return &rtq.Receiver{
		Addr:       addr,
		TLSConfig:  generateTLSConfig(),
		QUICConfig: quicConf,
		Codec:      codec,
		CC:         cc,
	}, nil
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}
}
