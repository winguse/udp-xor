package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"time"
)

var from = flag.String("from", "127.0.0.1:2000", "from addr")
var to = flag.String("to", "127.0.0.1:3000", "to addr")
var xorFlag = flag.Int("xor", 0, "the xor value for simple encode, only using first 8 bit.")

const (
	BUFF_SIZE = 1500
	TIMEOUT   = time.Second * 30
)

type Session struct {
	clientAddr *net.UDPAddr
	serverConn *net.UDPConn
}

type Forwarder struct {
	fromAddr  *net.UDPAddr
	toAddr    *net.UDPAddr
	localConn *net.UDPConn
	sessions  map[string]*Session
}

func xor(data []byte, n int) []byte {
	xor := byte(*xorFlag)
	for i := 0; i < n; i++ {
		data[i] = data[i] ^ xor
	}
	return data
}

func handleSession(f *Forwarder, key string, session *Session) {
	log.Printf("%s started", key)
	data := make([]byte, BUFF_SIZE)
	for {
		session.serverConn.SetReadDeadline(time.Now().Add(TIMEOUT))
		if n, _, err := session.serverConn.ReadFromUDP(data); err != nil {
			log.Printf("Error while read from server, %s", err)
			break
		} else if _, err := f.localConn.WriteToUDP(xor(data, n)[:n], session.clientAddr); err != nil {
			log.Printf("Error while write to client, %s", err)
			break
		} else {
			log.Printf("Sended %d bytes to %s\n", n, session.clientAddr.String())
		}
	}
	delete(f.sessions, key)
	log.Printf("%s ended", key)
}

func receivingFromClient(f *Forwarder) {
	data := make([]byte, BUFF_SIZE)
	for {
		n, clientAddr, err := f.localConn.ReadFromUDP(data)
		if err != nil {
			log.Printf("error during read: %s", err)
			continue
		}
		xor(data, n)
		log.Printf("<%s> size: %d\n", clientAddr, n)
		key := clientAddr.String()
		if session, found := f.sessions[key]; found {
			log.Printf("(old) Write to %s\n", f.toAddr.String())
			_, err := session.serverConn.Write(data[:n])
			if err != nil {
				log.Printf("Error while write to server, %s", err)
			}
			session.serverConn.SetReadDeadline(time.Now().Add(TIMEOUT))
		} else if serverConn, err := net.DialUDP("udp", nil, f.toAddr); err == nil {
			log.Printf("(new) Write to %s\n", f.toAddr.String())
			_, err := serverConn.Write(data[:n])
			if err != nil {
				log.Printf("Error while write to server (init), %s", err)
			}
			session := Session{
				clientAddr: clientAddr,
				serverConn: serverConn,
			}
			f.sessions[key] = &session
			go handleSession(f, key, &session)
		} else {
			log.Printf("Error while create server conn, %s", err)
		}
	}
}

func forward(from string, to string) (*Forwarder, error) {

	fromAddr, err := net.ResolveUDPAddr("udp", from)
	if err != nil {
		return nil, err
	}

	toAddr, err := net.ResolveUDPAddr("udp", to)
	if err != nil {
		return nil, err
	}

	localConn, err := net.ListenUDP("udp", fromAddr)
	if err != nil {
		return nil, err
	}

	f := Forwarder{
		fromAddr:  fromAddr,
		toAddr:    toAddr,
		localConn: localConn,
		sessions:  make(map[string]*Session),
	}

	log.Printf("Local: <%s> \n", localConn.LocalAddr().String())

	go receivingFromClient(&f)

	return &f, nil
}

func WaitForCtrlC() {
	var end_waiter sync.WaitGroup
	end_waiter.Add(1)
	var signal_channel chan os.Signal
	signal_channel = make(chan os.Signal, 1)
	signal.Notify(signal_channel, os.Interrupt)
	go func() {
		<-signal_channel
		end_waiter.Done()
	}()
	end_waiter.Wait()
}

func main() {
	flag.Parse()
	_, err := forward(*from, *to)
	if err != nil {
		log.Printf("Error while create fw, %s", err)
	}
	WaitForCtrlC()
}
