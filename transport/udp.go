package transport

import (
	"fmt"
	"net"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

type UDP struct {
	*net.UDPConn
	addr    net.Addr
	writers []*UDPWriteFlowCloser
}

func NewUDPServer(addr string) (*UDP, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", a)
	if err != nil {
		return nil, err
	}

	return &UDP{
		UDPConn: conn,
	}, nil
}

func NewUDPClient(addr string) (*UDP, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, a)
	if err != nil {
		return nil, err
	}
	return &UDP{
		addr:    a,
		UDPConn: conn,
	}, nil
}

func (u *UDP) Writer(id uint64) (*UDPWriteFlowCloser, error) {
	w := UDPWriteFlowCloser{UDP: u}
	u.writers = append(u.writers, &w)
	return &w, nil
}

func (u *UDP) setAddr(a net.Addr) {
	u.addr = a
	for _, w := range u.writers {
		w.addr = a
	}
}

func (u *UDP) Reader(id uint64) (*UDPReadFlowCloser, error) {
	return &UDPReadFlowCloser{UDP: u, setUDPAddr: u.setAddr}, nil
}

type UDPWriteFlowCloser struct {
	*UDP
	addr net.Addr
}

func (u *UDPWriteFlowCloser) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerBuf, err := header.Marshal()
	if err != nil {
		return 0, err
	}
	return u.UDP.Write(append(headerBuf, payload...))
}

func (u *UDPWriteFlowCloser) WriteRTCP(pkts []rtcp.Packet) (int, error) {
	buf, err := rtcp.Marshal(pkts)
	if err != nil {
		return 0, err
	}
	return u.WriteTo(buf, u.addr)
}

func (u *UDPWriteFlowCloser) Close() error {
	_, err := u.Write([]byte("eos"))
	if err != nil {
		return err
	}
	return u.UDPConn.Close()
}

type UDPReadFlowCloser struct {
	*UDP
	addr       net.Addr
	setUDPAddr func(net.Addr)
}

func (u *UDPReadFlowCloser) Read(p []byte) (n int, err error) {
	n, addr, err := u.UDPConn.ReadFrom(p)
	if addr != u.addr {
		u.addr = addr
		u.setUDPAddr(addr)
	}
	return n, err
}

func (u *UDPReadFlowCloser) Close() error {
	_, err := u.WriteTo([]byte("eos"), u.addr)
	if err != nil {
		return fmt.Errorf("failed to send EOS: %w", err)
	}
	return u.UDPConn.Close()
}
