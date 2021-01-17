package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/unixpickle/essentials"
)

type Proxy struct {
	Addr       string
	TargetAddr string
	LogDir     string
	TCPTimeout time.Duration
	FileLock   sync.Mutex
}

func main() {
	proxy := &Proxy{}
	flag.StringVar(&proxy.Addr, "addr", ":23778", "address to listen on")
	flag.StringVar(&proxy.TargetAddr, "target", "cm-ge.xlink.cn:23778", "address to proxy to")
	flag.StringVar(&proxy.LogDir, "log-dir", "saved-packets", "output directory to dump packets")
	flag.DurationVar(&proxy.TCPTimeout, "packet-time", time.Second/4,
		"max time between contents of TCP 'packet'")
	flag.Parse()

	go proxy.RunUDP()
	go proxy.RunTCP()
	select {}
}

func (p *Proxy) RunUDP() {
	udpAddr, err := net.ResolveUDPAddr("udp", p.Addr)
	essentials.Must(err)
	udpConn, err := net.ListenUDP("udp", udpAddr)
	essentials.Must(err)
	defer udpConn.Close()

	log.Printf("listening on UDP address %s...", udpAddr)

	outgoing := map[string]*net.UDPConn{}
	remoteAddr, err := net.ResolveUDPAddr("udp", p.TargetAddr)
	essentials.Must(err)

	for {
		b := make([]byte, 0x10000)
		oob := make([]byte, 0x10000)
		n, oobn, _, addr, err := udpConn.ReadMsgUDP(b, oob)
		essentials.Must(err)

		addrStr := addr.String()
		if _, ok := outgoing[addrStr]; !ok {
			proxyConn, err := net.DialUDP("udp", nil, remoteAddr)
			essentials.Must(err)
			outgoing[addrStr] = proxyConn

			go func() {
				for {
					b := make([]byte, 0x10000)
					oob := make([]byte, 0x10000)
					n, oobn, _, _, err := proxyConn.ReadMsgUDP(b, oob)
					essentials.Must(err)
					_, _, err = udpConn.WriteMsgUDP(b[:n], oob[:oobn], addr)
					essentials.Must(err)

					p.SavePacket("udp", addr, "out", b[:n])
					if oobn > 0 {
						p.SavePacket("udp", addr, "out_oob", oob[:oobn])
					}
				}
			}()
		}
		proxyConn := outgoing[addrStr]
		_, _, err = proxyConn.WriteMsgUDP(b[:n], oob[:oobn], addr)
		essentials.Must(err)

		p.SavePacket("udp", addr, "in", b[:n])
		if oobn > 0 {
			p.SavePacket("udp", addr, "in_oob", oob[:oobn])
		}
	}
}

func (p *Proxy) RunTCP() {
	tcpAddr, err := net.ResolveTCPAddr("tcp", p.Addr)
	essentials.Must(err)
	listener, err := net.ListenTCP("tcp", tcpAddr)
	essentials.Must(err)
	defer listener.Close()

	log.Printf("listening on TCP address %s...", tcpAddr)

	for {
		conn, err := listener.AcceptTCP()
		essentials.Must(err)
		go p.HandleTCP(conn)
	}
}

func (p *Proxy) HandleTCP(conn *net.TCPConn) {
	log.Printf("TCP connection established from: %s", conn.RemoteAddr())
	defer log.Printf("TCP connection finished: %s", conn.RemoteAddr())

	remoteAddr, err := net.ResolveTCPAddr("tcp", p.TargetAddr)
	essentials.Must(err)
	proxyConn, err := net.DialTCP("tcp", nil, remoteAddr)
	essentials.Must(err)

	defer proxyConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer proxyConn.Close()
		for inPacket := range ReceiveBursts(conn, p.TCPTimeout) {
			p.SavePacket("tcp", conn.RemoteAddr(), "in", inPacket)
			if WriteOrClose(proxyConn, inPacket) != nil {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer conn.Close()
		for outPacket := range ReceiveBursts(proxyConn, p.TCPTimeout) {
			p.SavePacket("tcp", conn.RemoteAddr(), "out", outPacket)
			if WriteOrClose(conn, outPacket) != nil {
				return
			}
		}
	}()

	wg.Wait()
}

func (p *Proxy) SavePacket(network string, addr fmt.Stringer, direction string, data []byte) {
	log.Printf("packet: net=%s addr=%s direction=%s size=%d", network, addr, direction, len(data))

	outDir := filepath.Join(p.LogDir, network, addr.String())
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		essentials.Must(os.MkdirAll(outDir, 0755))
	} else {
		essentials.Must(err)
	}

	// Don't want any other writer to conflict to find the
	// next file index.
	p.FileLock.Lock()
	defer p.FileLock.Unlock()

	listing, err := ioutil.ReadDir(outDir)
	essentials.Must(err)
	nextIdx := 0
	for _, entry := range listing {
		name := entry.Name()
		parts := strings.Split(name, "_")
		if len(parts) != 2 {
			continue
		}
		x, err := strconv.Atoi(parts[0])
		if err == nil {
			nextIdx = essentials.MaxInt(nextIdx, x+1)
		}
	}
	outName := fmt.Sprintf("%06d_%s", nextIdx, direction)
	outFile := filepath.Join(outDir, outName)
	essentials.Must(ioutil.WriteFile(outFile, data, 0644))
}
