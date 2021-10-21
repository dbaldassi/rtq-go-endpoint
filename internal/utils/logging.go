package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/lucas-clemente/quic-go/logging"
)

type NopCloser struct {
	io.Writer
}

func (c NopCloser) Close() error {
	return nil
}

func getFileLogWriter(path string) (io.WriteCloser, error) {
	logfile, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return logfile, nil
	//return newBufferedWriteCloser(bufio.NewWriter(logfile), logfile), nil
}

func GetMainLogWriter() (io.WriteCloser, error) {
	logFilename := os.Getenv("LOG_FILE")
	if len(logFilename) == 0 {
		return NopCloser{Writer: os.Stdout}, nil
	}
	return getFileLogWriter(logFilename)
}

func GetCCStatLogWriter() (io.WriteCloser, error) {
	logFilename := os.Getenv("CCLOGFILE")
	if len(logFilename) == 0 {
		return NopCloser{Writer: os.Stdout}, nil
	}
	return getFileLogWriter(logFilename)
}

func GetStreamLogWriter() (io.WriteCloser, error) {
	logFilename := os.Getenv("STREAMLOGFILE")
	if len(logFilename) == 0 {
		return NopCloser{Writer: os.Stdout}, nil
	}
	return getFileLogWriter(logFilename)
}

// GetQLOGWriter creates the QLOGDIR and returns the GetLogWriter callback
func GetQLOGWriter() (func(perspective logging.Perspective, connID []byte) io.WriteCloser, error) {
	qlogDir := os.Getenv("QLOGDIR")
	if len(qlogDir) == 0 {
		return nil, nil
	}
	_, err := os.Stat(qlogDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(qlogDir, 0o666); err != nil {
				return nil, fmt.Errorf("failed to create qlog dir %s: %v", qlogDir, err)
			}
		} else {
			return nil, err
		}
	}
	return func(_ logging.Perspective, connID []byte) io.WriteCloser {
		path := fmt.Sprintf("%s/%x.qlog", strings.TrimRight(qlogDir, "/"), connID)
		w, err := getFileLogWriter(path)
		if err != nil {
			log.Printf("failed to create qlog file %s: %v", path, err)
			return nil
		}
		log.Printf("created qlog file: %s\n", path)
		return w
	}, nil
}

func GetRTPLogWriter() (func(string) io.WriteCloser, error) {
	rtpLogDir := os.Getenv("RTPLOGDIR")
	if len(rtpLogDir) == 0 {
		return func(string) io.WriteCloser {
			return NopCloser{Writer: os.Stdout}
		}, nil
	}
	_, err := os.Stat(rtpLogDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(rtpLogDir, os.ModePerm); err != nil {
				return nil, fmt.Errorf("failed to create qlog dir %s: %v", rtpLogDir, err)
			}
		} else {
			return nil, err
		}
	}
	return func(stream string) io.WriteCloser {
		path := fmt.Sprintf("%s/%s.log", strings.TrimRight(rtpLogDir, "/"), stream)
		w, err := getFileLogWriter(path)
		if err != nil {
			log.Printf("failed to create rtp/rtcp log file %s: %v", path, err)
			return nil
		}
		log.Printf("created rtp/rtcp log file: %s", path)
		return w
	}, nil
}
