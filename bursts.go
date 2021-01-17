package main

import (
	"net"
	"time"
)

// ReceiveBursts reads "packets" (as separated by time of
// no data transfer) and sends them to a channel.
func ReceiveBursts(conn *net.TCPConn, maxTime time.Duration) <-chan []byte {
	// Use a larger buffer to attempt to prevent backpressure from
	// hurting our ability to measure time.
	// It can still happen though if there is a lot of data being
	// received.
	res := make(chan []byte, 64)
	go func() {
		defer close(res)
		rawData := readConnAsChan(conn)
		for {
			buffer, ok := <-rawData
			if !ok {
				break
			}
		ReadMoreLoop:
			for {
				select {
				case data, ok := <-rawData:
					if ok {
						buffer = append(buffer, data...)
					} else {
						break ReadMoreLoop
					}
				case <-time.After(maxTime):
					break ReadMoreLoop
				}
			}
			res <- buffer
		}
	}()
	return res
}

func readConnAsChan(conn *net.TCPConn) <-chan []byte {
	rawReads := make(chan []byte, 64)

	// Goroutine to read the raw data.
	go func() {
		defer close(rawReads)
		for {
			data := make([]byte, 0x10000)
			n, err := conn.Read(data)
			if err != nil {
				return
			}
			rawReads <- data[:n]
		}
	}()

	return rawReads
}

// WriteOrClose writes a buffer to a socket, and closes the
// socket if the write fails.
func WriteOrClose(conn *net.TCPConn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		data = data[n:]
		if err != nil {
			conn.Close()
			return err
		}
	}
	return nil
}
