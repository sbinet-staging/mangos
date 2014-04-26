// Copyright 2014 Garrett D'Amore
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use file except in compliance with the License.
// You may obtain a copy of the license at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// macat implements a nanocat(1) workalike command.
package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"
)

import (
	"bitbucket.org/gdamore/mangos"
	"bitbucket.org/gdamore/mangos/protocol/bus"
	"bitbucket.org/gdamore/mangos/protocol/pair"
	"bitbucket.org/gdamore/mangos/protocol/pub"
	"bitbucket.org/gdamore/mangos/protocol/pull"
	"bitbucket.org/gdamore/mangos/protocol/push"
	"bitbucket.org/gdamore/mangos/protocol/rep"
	"bitbucket.org/gdamore/mangos/protocol/req"
	"bitbucket.org/gdamore/mangos/protocol/respondent"
	"bitbucket.org/gdamore/mangos/protocol/star"
	"bitbucket.org/gdamore/mangos/protocol/sub"
	"bitbucket.org/gdamore/mangos/protocol/surveyor"
	"bitbucket.org/gdamore/mangos/transport/tcp"
	"bitbucket.org/gdamore/mangos/transport/inproc"
	"bitbucket.org/gdamore/mangos/transport/ipc"
	"bitbucket.org/gdamore/mangos/transport/tlstcp"
	"github.com/droundy/goopt"
)

var verbose int
var protoSet bool
var proto string
var dialAddrs []string
var listenAddrs []string
var subscriptions []string
var recvTimeout int
var sendTimeout int
var sendInterval int
var sendDelay int
var sendData []byte
var printFormat string
var sock mangos.Socket

func setSocket(f func() (mangos.Socket, error)) error {
	var err error
	if sock != nil {
		return errors.New("protocol already selected")
	}
	sock, err = f()

	sock.AddTransport(inproc.NewTransport())
	sock.AddTransport(ipc.NewTransport())
	sock.AddTransport(tcp.NewTransport())
	sock.AddTransport(tlstcp.NewTransport())
	return err
}

func addDial(addr string) error {
	if !strings.Contains(addr, "://") {
		return errors.New("invalid address format")
	}
	dialAddrs = append(dialAddrs, addr)
	return nil
}

func addListen(addr string) error {
	if !strings.Contains(addr, "://") {
		return errors.New("invalid address format")
	}
	listenAddrs = append(listenAddrs, addr)
	return nil
}

func addListenIPC(path string) error {
	return addListen("ipc://" + path)
}

func addDialIPC(path string) error {
	return addDial("ipc://" + path)
}

func addListenLocal(port string) error {
	return addListen("tcp://127.0.0.1:" + port)
}

func addDialLocal(port string) error {
	return addDial("tcp://127.0.0.1:" + port)
}

func addSub(sub string) error {
	subscriptions = append(subscriptions, sub)
	return nil
}

func setSendData(data string) error {
	if sendData != nil {
		return errors.New("data or file already set")
	}
	sendData = []byte(data)
	return nil
}

func setSendFile(path string) error {
	if sendData != nil {
		return errors.New("data or file already set")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	sendData, err = ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	return nil
}

func setFormat(f string) error {
	if len(printFormat) > 0 {
		return errors.New("output format already set")
	}
	switch f {
	case "no":
	case "raw":
	case "ascii":
	case "quoted":
	case "msgpack":
	default:
		return errors.New("invalid format type")
	}
	printFormat = f
	return nil
}

func fatalf(format string, v ...interface{}) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, v...))
	os.Exit(1)
}

func init() {

	goopt.NoArg([]string{"--verbose", "-v"}, "Increase verbosity",
		func() error {
			verbose++
			return nil
		})
	goopt.NoArg([]string{"--silent", "-q"}, "Decrease verbosity",
		func() error {
			verbose--
			return nil
		})

	goopt.NoArg([]string{"--push"}, "Use PUSH socket type", func() error {
		return setSocket(push.NewSocket)
	})
	goopt.NoArg([]string{"--pull"}, "Use PULL socket type", func() error {
		return setSocket(pull.NewSocket)
	})
	goopt.NoArg([]string{"--pub"}, "Use PUB socket type", func() error {
		return setSocket(pub.NewSocket)
	})
	goopt.NoArg([]string{"--sub"}, "Use SUB socket type", func() error {
		return setSocket(sub.NewSocket)
	})
	goopt.NoArg([]string{"--req"}, "Use REQ socket type", func() error {
		return setSocket(req.NewSocket)
	})
	goopt.NoArg([]string{"--rep"}, "Use REP socket type", func() error {
		return setSocket(rep.NewSocket)
	})
	goopt.NoArg([]string{"--surveyor"}, "Use SURVEYOR socket type",
		func() error {
			return setSocket(surveyor.NewSocket)
		})
	goopt.NoArg([]string{"--respondent"}, "Use RESPONDENT socket type",
		func() error {
			return setSocket(respondent.NewSocket)
		})
	goopt.NoArg([]string{"--bus"}, "Use BUS socket type", func() error {
		return setSocket(bus.NewSocket)
	})
	goopt.NoArg([]string{"--pair"}, "Use PAIR socket type", func() error {
		return setSocket(pair.NewSocket)
	})
	goopt.NoArg([]string{"--star"}, "Use STAR socket type", func() error {
		return setSocket(star.NewSocket)
	})
	goopt.ReqArg([]string{"--bind"}, "ADDR", "Bind socket to ADDR",
		addListen)
	goopt.ReqArg([]string{"--connect"}, "ADDR", "Connect socket to ADDR",
		addDial)
	goopt.ReqArg([]string{"--bind-ipc", "-X"}, "PATH",
		"Bind socket to IPC PATH", addListenIPC)
	goopt.ReqArg([]string{"--connect-ipc", "-x"}, "PATH",
		"Connect socket to IPC PATH", addDialIPC)
	goopt.ReqArg([]string{"--bind-local", "-L"}, "PORT",
		"Bind socket to TCP localhost PORT", addListenLocal)
	goopt.ReqArg([]string{"--connect-local", "-l"}, "PORT",
		"Connect socket to TCP localhost PORT", addDialLocal)
	goopt.ReqArg([]string{"--subscribe"}, "PREFIX",
		"Subcribe to PREFIX (default is wildcard)", addSub)
	goopt.ReqArg([]string{"--recv-timeout"}, "SEC", "Set receive timeout",
		func(to string) error {
			var err error
			recvTimeout, err = strconv.Atoi(to)
			if err != nil {
				return errors.New("value not an integer")
			}
			return nil
		})
	goopt.ReqArg([]string{"--send-timeout"}, "SEC", "Set send timeout",
		func(to string) error {
			var err error
			if sendTimeout, err = strconv.Atoi(to); err != nil {
				return errors.New("value not an integer")
			}
			return nil
		})
	goopt.ReqArg([]string{"--send-delay", "-d"}, "SEC",
		"Set initial send delay",
		func(to string) error {
			var err error
			if sendDelay, err = strconv.Atoi(to); err != nil {
				return errors.New("value not an integer")
			}
			return nil
		})
	goopt.NoArg([]string{"--raw"}, "Raw output, no delimiters",
		func() error {
			return setFormat("raw")
		})
	goopt.NoArg([]string{"--ascii", "-A"}, "ASCII output, one per line",
		func() error {
			return setFormat("ascii")
		})
	goopt.NoArg([]string{"--quoted", "-Q"}, "Quoted output, one per line",
		func() error {
			return setFormat("quoted")
		})
	goopt.NoArg([]string{"--msgpack"},
		"Msgpacked binay output (see msgpack.org)",
		func() error {
			return setFormat("msgpack")
		})

	goopt.ReqArg([]string{"--interval", "-i"}, "SEC",
		"Send DATA every SEC seconds",
		func(to string) error {
			var err error
			if sendInterval, err = strconv.Atoi(to); err != nil {
				return errors.New("value not an integer")
			}
			return nil
		})

	goopt.ReqArg([]string{"--data", "-D"}, "DATA", "Data to send",
		setSendData)
	goopt.ReqArg([]string{"--file", "-F"}, "FILE", "Send contents of FILE",
		setSendFile)

	goopt.Description = func() string {
		return `macat is a command-line interface to send and receive
data via the mangos implementation of the SP (nanomsg) protocols.  It is
designed to be suitable for use as a drop-in replacement for nanocat(1). `
	}

	goopt.Author = "Garrett D'Amore"

	goopt.Suite = "mangos"

	goopt.Summary = "command line interface to the mangos messaging"

}

func printMsg(msg *mangos.Message) {
	bw := bufio.NewWriter(os.Stdout)
	switch printFormat {
	case "no":
		return
	case "raw":
		bw.Write(msg.Body)
	case "ascii":
		for i := 0; i < len(msg.Body); i++ {
			if strconv.IsPrint(rune(msg.Body[i])) {
				bw.WriteByte(msg.Body[i])
			} else {
				bw.WriteByte('.')
			}
		}
		bw.WriteString("\n")
	case "quoted":
		for i := 0; i < len(msg.Body); i++ {
			switch msg.Body[i] {
			case '\n':
				bw.WriteString("\\n")
			case '\r':
				bw.WriteString("\\r")
			case '\\':
				bw.WriteString("\\\\")
			case '"':
				bw.WriteString("\\\"")
			default:
				if strconv.IsPrint(rune(msg.Body[i])) {
					bw.WriteByte(msg.Body[i])
				} else {
					bw.WriteString(fmt.Sprintf("\\x%02x",
						msg.Body[i]))
				}
			}
		}
		bw.WriteString("\n")

	case "msgpack":
		enc := make([]byte, 5)
		switch {
		case len(msg.Body) < 256:
			enc = enc[:2]
			enc[0] = 0xc4
			enc[1] = byte(len(msg.Body))

		case len(msg.Body) < 65536:
			enc = enc[:3]
			enc[0] = 0xc5
			binary.BigEndian.PutUint16(enc[1:], uint16(len(msg.Body)))
		default:
			enc = enc[:5]
			enc[0] = 0xc6
			binary.BigEndian.PutUint32(enc[1:], uint32(len(msg.Body)))
		}
		bw.Write(enc)
		bw.Write(msg.Body)
	}
	bw.Flush()
}

func recvLoop(sock mangos.Socket, done chan struct{}) {
	defer close(done)
	for {
		msg, err := sock.RecvMsg()
		switch err {
		case mangos.ErrRecvTimeout:
			return
		case nil:
		default:
			fatalf("RecvMsg failed: %v", err)
		}
		printMsg(msg)
		msg.Free()
	}
}

func sendLoop(sock mangos.Socket, done chan struct{}) {
	defer close(done)
	if sendData == nil {
		fatalf("No data to send!")
	}
	for {
		msg := mangos.NewMessage(len(sendData))
		msg.Body = append(msg.Body, sendData...)
		err := sock.SendMsg(msg)

		if err != nil {
			fatalf("SendMsg failed: %v", err)
		}

		if sendInterval > 0 {
			time.Sleep(time.Duration(sendInterval) * time.Second)
		} else {
			break
		}
	}
}

func replyLoop(sock mangos.Socket, done chan struct{}) {
	defer close(done)
	if sendData == nil {
		fatalf("No data to send!")
	}
	for {
		msg, err := sock.RecvMsg()
		switch err {
		case mangos.ErrRecvTimeout:
			return
		case nil:
		default:
			fatalf("RecvMsg failed: %v", err)
		}
		printMsg(msg)
		msg.Free()

		msg = mangos.NewMessage(len(sendData))
		msg.Body = append(msg.Body, sendData...)
		err = sock.SendMsg(msg)

		if err != nil {
			fatalf("SendMsg failed: %v", err)
		}
	}
}

func cleanup() {
	if sock != nil {
		sock.Close()
	}
}

func main() {
	defer cleanup()

	goopt.Parse(nil)

	if sock == nil {
		fatalf("Protocol not specified.")
	}
	if len(listenAddrs) == 0 && len(dialAddrs) == 0 {
		fatalf("No address specified.")
	}

	if sock.GetProtocol().Number() != mangos.ProtoSub {
		if len(subscriptions) > 0 {
			fatalf("Subscriptions only valid with SUB type sockets.")
		}
	} else {
		if len(subscriptions) > 0 {
			for i := range subscriptions {
				err := sock.SetOption(mangos.OptionSubscribe,
					subscriptions[i])
				if err != nil {
					fatalf("Can't subscribe: %v", err)
				}
			}
		} else {
			err := sock.SetOption(mangos.OptionSubscribe, []byte{})
			if err != nil {
				fatalf("Can't wild card subscribe: %v", err)
			}
		}
	}

	for i := range listenAddrs {
		err := sock.Listen(listenAddrs[i])
		if err != nil {
			fatalf("Bind(%s): %v", listenAddrs[i], err)
		}
	}

	for i := range dialAddrs {
		err := sock.Dial(dialAddrs[i])
		if err != nil {
			fatalf("Dial(%s): %v", dialAddrs[i], err)
		}
	}

	time.Sleep(time.Second * time.Duration(sendDelay))

	rxdone := make(chan struct{})
	txdone := make(chan struct{})

	// Start main processing
	switch sock.GetProtocol().Number() {
	case mangos.ProtoPull:
		fallthrough
	case mangos.ProtoSub:
		go recvLoop(sock, rxdone)
		close(txdone)
	case mangos.ProtoPush:
		fallthrough
	case mangos.ProtoPub:
		go sendLoop(sock, txdone)
		close(rxdone)
	case mangos.ProtoPair:
		fallthrough
	case mangos.ProtoStar:
		fallthrough
	case mangos.ProtoBus:
		if sendData != nil {
			go sendLoop(sock, txdone)
		} else {
			close(txdone)
		}
		go recvLoop(sock, rxdone)
	case mangos.ProtoSurveyor:
		fallthrough
	case mangos.ProtoReq:
		go sendLoop(sock, txdone)
		go recvLoop(sock, rxdone)
	case mangos.ProtoRep:
		fallthrough
	case mangos.ProtoRespondent:
		if sendData != nil {
			go replyLoop(sock, rxdone)
		} else {
			go recvLoop(sock, rxdone)
			close(txdone)
		}
	default:
		fatalf("Unknown protocol!")
	}

	// Wait for threads to exit
	select {
	case <-rxdone:
	}

	select {
	case <-txdone:
	}
}