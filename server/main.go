package main

import (
	"fmt"
	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
	"github.com/scionproto/scion/pkg/private/common"
	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/snet"
	"github.com/scionproto/scion/pkg/snet/metrics"
	"net"
	"os"
)

// Should I use a python environment?

// sudo apt install python3-pip

// alias docker-compose='docker compose'

// ./scion.sh bazel_remote
// systemctl restart docker.service
// sudo apt install ethtool
// make test-integration
// sudo update-alternatives --install /usr/local/bin/python python /usr/bin/python3 1
// sudo apt install ssh-askpass-gnome
// export SUDO_ASKPASS=/usr/bin/ssh-askpass

// sudo vi /etc/sudoers
// # Allow members of group sudo to execute any command
// #%sudo  ALL=(ALL:ALL) ALL
// %sudo ALL = (ALL) NOPASSWD: ALL

import (
	"context"
)

var (
	scionPacketConnMetrics = metrics.NewSCIONPacketConnMetrics()
	scmpErrorsCounter      = scionPacketConnMetrics.SCMPErrors
)

func main() {
	os.Exit(realMain())
}

// Without dispatcher
func realMain() int {
	fmt.Println("Starting server ...")

	ctx := context.Background()

	//fmt.Print("Connecting to dispatcher ... ")
	//disp := reliable.NewDispatcher("")
	//fmt.Println("dispatcher connected:", disp)

	//daemonAddr := "127.0.0.1:30255" // Default address from daemon.go
	//daemonAddr := "[fd00:f00d:cafe::7f00:a]:31010" // from 112-topo
	daemonAddr := "[fd00:f00d:cafe::7f00:b]:30255" // from 112 topo
	fmt.Print("Connecting to daemon: ", daemonAddr, " ... ")
	// TODO the following is deprecated
	daemonConn, err := daemon.NewService(daemonAddr).Connect(ctx)
	checkErr(err, "Error connecting to daemon")
	fmt.Println("done")

	revHandler := daemon.RevHandler{Connector: daemonConn}

	fmt.Print("Connection factory: ... ")
	connector := &snet.DefaultConnector{
		SCMPHandler: snet.DefaultSCMPHandler{
			RevocationHandler: revHandler,
			SCMPErrors:        scmpErrorsCounter,
		},
		Metrics: scionPacketConnMetrics,
	}
	fmt.Println(" done")

	// register
	localIA, err := addr.ParseIA("1-ff00:0:112")
	checkError(err)
	//localAddr, err := net.ResolveUDPAddr("udp", "[127.0.0.111]:8080")
	localAddr, err := net.ResolveUDPAddr("udp", "[::1]:8080")
	checkError(err)
	fmt.Print("Registering ... ")
	conn, err := connector.OpenUDP(localAddr)
	defer conn.Close()
	checkErr(err, "Error registering")
	fmt.Println(" done")

	fmt.Printf("Connected as: %v,[%v]:%d \n", localIA, localAddr.IP, localAddr.Port)

	for true {
		err = handlePing(conn)
		checkError(err)
	}
	return 0
}

// WIth dispatcher
//func realMain() int {
//	fmt.Println("Starting server ...")
//
//	ctx := context.Background()
//
//	fmt.Print("Connecting to dispatcher ... ")
//	disp := reliable.NewDispatcher("")
//	fmt.Println("dispatcher connected:", disp)
//
//	//daemonAddr := "127.0.0.1:30255" // Default address from daemon.go
//	//daemonAddr := "[fd00:f00d:cafe::7f00:a]:31010" // from 112-topo
//	daemonAddr := "[fd00:f00d:cafe::7f00:b]:30255" // from 112 topo
//	fmt.Print("Connecting to daemon: ", daemonAddr, " ... ")
//	// TODO the following is deprecated
//	daemonConn, err := daemon.NewService(daemonAddr).Connect(ctx)
//	checkErr(err, "Error connecting to daemon")
//	fmt.Println("done")
//
//	revHandler := daemon.RevHandler{Connector: daemonConn}
//
//	fmt.Print("Connection factory: ... ")
//	connFactory := &snet.DefaultPacketDispatcherService{
//		Dispatcher: disp,
//		SCMPHandler: snet.DefaultSCMPHandler{
//			RevocationHandler: revHandler,
//			SCMPErrors:        scmpErrorsCounter,
//		},
//		SCIONPacketConnMetrics: scionPacketConnMetrics,
//	}
//	fmt.Println(" done")
//
//	// register
//	localIA, err := addr.ParseIA("1-ff00:0:112")
//	checkError(err)
//	//localAddr, err := net.ResolveUDPAddr("udp", "[127.0.0.111]:8080")
//	localAddr, err := net.ResolveUDPAddr("udp", "[::1]:8080")
//	checkError(err)
//	fmt.Print("Registering ... ")
//	conn, port, err := connFactory.Register(context.Background(), localIA, localAddr, addr.SvcNone)
//	defer conn.Close()
//	checkErr(err, "Error registering")
//	fmt.Println(" done")
//
//	fmt.Printf("Connected as: %v,[%v]:%d \n", localIA, localAddr.IP, port)
//
//	for true {
//		err = handlePing(conn)
//		checkError(err)
//	}
//	return 0
//}

func handlePing(conn snet.PacketConn) error {
	var p snet.Packet
	var ov net.UDPAddr
	fmt.Print("Waiting ... ")
	err := conn.ReadFrom(&p, &ov)
	checkErr(err, "Error reading packet")
	fmt.Println("received packet")

	udp, ok := p.Payload.(snet.UDPPayload)
	checkOk(ok, "Error reading payload")

	fmt.Printf("Received message: \"%s\" from %v:%v\n", string(udp.Payload), ov.IP, udp.SrcPort)

	p.Destination, p.Source = p.Source, p.Destination
	p.Payload = snet.UDPPayload{
		DstPort: udp.SrcPort,
		SrcPort: udp.DstPort,
		// Payload: []byte("Re: " + string(udp.Payload)),
		Payload: udp.Payload,
	}

	// reverse path
	rpath, ok := p.Path.(snet.RawPath)
	if !ok {
		return serrors.New("unecpected path", "type", common.TypeOf(p.Path))
	}

	replypather := snet.DefaultReplyPather{}
	replyPath, err := replypather.ReplyPath(rpath)
	if err != nil {
		return serrors.WrapStr("creating reply path", err)
	}
	p.Path = replyPath
	// Send pong
	if err := conn.WriteTo(&p, &ov); err != nil {
		return serrors.WrapStr("sending reply", err)
	}

	fmt.Println("pkt bytes: ", p.Bytes)
	fmt.Println("Sent answer to:", p.Destination)
	return nil
}

func checkErr(err error, msg string) {
	if err != nil {
		fmt.Println(msg, ": ", err)
		os.Exit(1)
	}
}

func checkError(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func checkOk(ok bool, msg string) {
	if !ok {
		fmt.Println(msg)
		os.Exit(1)
	}
}
