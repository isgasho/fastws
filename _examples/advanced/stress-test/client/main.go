package main

import (
	"bufio"
	"bytes"
	"context"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kataras/fastws/_examples/advanced/ws"
)

var (
	url      = "ws://localhost:8080"
	testdata []byte
)

const (
	totalClients = 100000 // max depends on the OS.
	verbose      = true
)

var connectionFailures uint64

var (
	disconnectErrors []error
	connectErrors    []error
	errMu            sync.Mutex
)

func collectError(op string, err error) {
	errMu.Lock()
	defer errMu.Unlock()

	switch op {
	case "disconnect":
		disconnectErrors = append(disconnectErrors, err)
	case "connect":
		connectErrors = append(connectErrors, err)
	}
}

func main() {
	log.Println("-- Running...")
	f, err := os.Open("./test.data")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	testdata, err = ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}

	start := time.Now()

	wg := new(sync.WaitGroup)
	wg.Add(totalClients)

	relaxCPU := 15 * time.Second // this may not be useful if host contains high-end hardware.
	lastRelaxCPU := time.Now()
	var alive time.Duration
	for i := 0; i < totalClients; i++ {
		if i%2 == 0 {
			time.Sleep(time.Duration(rand.Int63n(4)) * time.Millisecond)
			alive = 2*time.Second - time.Duration(rand.Int63n(3))*time.Millisecond
		} else if i%3 == 0 {
			time.Sleep(time.Duration(rand.Int63n(6)) * time.Millisecond)
			alive = 3*time.Second - time.Duration(rand.Int63n(3))*time.Millisecond
		} else {
			alive = 4*time.Second - time.Duration(rand.Int63n(3))*time.Millisecond
		}

		if time.Now().After(lastRelaxCPU.Add(relaxCPU)) {
			time.Sleep(relaxCPU)
			lastRelaxCPU = time.Now()
		}

		go connect(wg, alive)
	}

	wg.Wait()

	log.Printf("execution time [%s]", time.Since(start))
	log.Println()

	if connectionFailures > 0 {
		log.Printf("Finished with %d/%d connection failures.", connectionFailures, totalClients)
	}

	if n := len(connectErrors); n > 0 {
		log.Printf("Finished with %d connect errors: ", n)
		var lastErr error
		var sameC int

		for i, err := range connectErrors {
			if lastErr != nil {
				if lastErr.Error() == err.Error() {
					sameC++
					continue
				} else {
					_, ok := lastErr.(*net.OpError)
					if ok {
						if _, ok = err.(*net.OpError); ok {
							sameC++
							continue
						}
					}
				}
			}

			if sameC > 0 {
				log.Printf("and %d more like this...\n", sameC)
				sameC = 0
				continue
			}

			log.Printf("[%d] - %v\n", i+1, err)
			lastErr = err
		}
	}

	if n := len(disconnectErrors); n > 0 {
		log.Printf("Finished with %d disconnect errors\n", n)
		for i, err := range disconnectErrors {
			log.Printf("[%d] - %v\n", i+1, err)
		}
	}

	if connectionFailures == 0 && len(connectErrors) == 0 && len(disconnectErrors) == 0 {
		log.Println("ALL OK.")
	}

	log.Println("-- Finished.")
}

// var msg = []byte("my message")

var counter uint32

func connect(wg *sync.WaitGroup, alive time.Duration) {
	defer wg.Done()

	// t := atomic.AddUint32(&counter, 1)

	// log.Printf("[%d] try to connect\n", t)
	// ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(alive))
	// defer cancel()
	ctx := context.Background()
	client, err := ws.Dial(ctx, url, ws.Events{
		"chat": func(c ws.NSConn, msg ws.Message) error {
			if verbose {
				log.Println(string(msg.Body))
			}
			return nil
		},
	})

	if err != nil {
		//	log.Printf("[%d] %s\n", t, err)
		atomic.AddUint64(&connectionFailures, 1)
		collectError("connect", err)
		return
		// return err
	}

	c := client.Connect("")

	// c.HandleFunc("chat", func(c ws.Conn, message []byte) error {
	// 	if verbose {
	// 		log.Println(string(message))
	// 	}
	// 	return nil
	// })

	r := ioutil.NopCloser(bytes.NewBuffer(testdata))
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if text := scanner.Bytes(); len(text) > 1 {
			c.Emit("chat", text)
		}
	}

	// time.Sleep(2 * time.Second)
	time.Sleep(alive)
	c.Close()
	// time.Sleep(alive)
}
