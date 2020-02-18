package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

var fw = flag.String("forward", "127.0.0.1:2000~127.0.0.1:3000", "can be multiple: from~to[,from~to[,from~to]]")
var xorFlag = flag.Int("xor", 0, "the xor value for simple encode, only using first 8 bit.")
var maxLen = flag.Int("max-len", 0x7fffffff, "the max length of xor from the package beginning")
var sessionTimeoutByRemoteOnly = flag.Bool("session-timeout-by-remote-only", false, "session timeout by remote reply only")
var timeout = flag.Int("timeout", 30, "session timeout in seconds")
var bufferSize = flag.Int("buffer-size", 1600, "buffer size in bytes, the max UDP package size.")
var verboseLoging = flag.Bool("verbose", false, "verbose logging")

// Session is the UDP session info
type Session struct {
	clientAddr *net.UDPAddr
	serverConn *net.UDPConn
}

// Forwarder is the info of forword
type Forwarder struct {
	fromAddr  *net.UDPAddr
	toAddr    *net.UDPAddr
	localConn *net.UDPConn
	sessions  map[string]*Session
}

func xor(data []byte, n int) []byte {
	xor := byte(*xorFlag)
	for i := 0; i < n && i < *maxLen; i++ {
		data[i] = data[i] ^ xor
	}
	return data
}

func verbosePrintf(format string, v ...interface{}) {
	if *verboseLoging {
		log.Printf(format, v...)
	}
}

func handleSession(f *Forwarder, key string, session *Session) {
	log.Printf("%s started", key)
	data := make([]byte, *bufferSize)
	for {
		session.serverConn.SetReadDeadline(time.Now().Add(time.Second * time.Duration(*timeout)))
		if n, _, err := session.serverConn.ReadFromUDP(data); err != nil {
			log.Printf("Error while read from server, %s", err)
			break
		} else if _, err := f.localConn.WriteToUDP(xor(data, n)[:n], session.clientAddr); err != nil {
			log.Printf("Error while write to client, %s", err)
			break
		} else {
			verbosePrintf("Sended %d bytes to %s\n", n, session.clientAddr.String())
		}
	}
	delete(f.sessions, key)
	log.Printf("%s ended", key)
}

func receivingFromClient(f *Forwarder) {
	data := make([]byte, *bufferSize)
	for {
		n, clientAddr, err := f.localConn.ReadFromUDP(data)
		if err != nil {
			log.Printf("error during read: %s", err)
			continue
		}
		xor(data, n)
		verbosePrintf("<%s> size: %d\n", clientAddr, n)
		key := clientAddr.String()
		if session, found := f.sessions[key]; found {
			verbosePrintf("(old) Write to %s\n", f.toAddr.String())
			_, err := session.serverConn.Write(data[:n])
			if err != nil {
				log.Printf("Error while write to server, %s", err)
			}
			if *sessionTimeoutByRemoteOnly == false {
				session.serverConn.SetReadDeadline(time.Now().Add(time.Second * time.Duration(*timeout)))
			}
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

	log.Printf("<%s> forward to <%s>\n", fromAddr.String(), toAddr.String())

	go receivingFromClient(&f)

	return &f, nil
}

// WaitForCtrlC to terminate the program
func WaitForCtrlC() {
	var endWaiter sync.WaitGroup
	endWaiter.Add(1)
	var signalChannel chan os.Signal
	signalChannel = make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt)
	go func() {
		<-signalChannel
		endWaiter.Done()
	}()
	endWaiter.Wait()
}

func main() {
	flag.Parse()
	for _, pair := range strings.Split(*fw, ",") {
		fromAndTo := strings.Split(pair, "~")
		if len(fromAndTo) != 2 {
			log.Printf("Invalid from,to %s", fromAndTo)
			break
		}
		_, err := forward(fromAndTo[0], fromAndTo[1])
		if err != nil {
			log.Printf("Error while create fw, %s", err)
			break
		}
	}
	WaitForCtrlC()
}
