package main

// Should I use a python environment?

// sudo apt install python3-pip

// alias docker-compose='docker compose'

// ./scion.sh bazel_remote
// systemctl restart docker.service

// make test-integration
// sudo update-alternatives --install /usr/local/bin/python python /usr/bin/python3 1
// sudo apt install ssh-askpass-gnome
// export SUDO_ASKPASS=/usr/bin/ssh-askpass

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
	"github.com/scionproto/scion/pkg/private/common"
	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/snet"
	"github.com/scionproto/scion/pkg/snet/metrics"
	"github.com/scionproto/scion/pkg/sock/reliable"
)

var (
	scionPacketConnMetrics = metrics.NewSCIONPacketConnMetrics()
	scmpErrorsCounter      = scionPacketConnMetrics.SCMPErrors
	DefaultIOTimeout       = 1 * time.Second
)

func main() {
	os.Exit(realMain())
}

func realMain() int {
	fmt.Println("Starting server ...")

	ctx := context.Background()

	fmt.Print("Connecting to dispatcher ... ")
	disp := reliable.NewDispatcher("")
	fmt.Println("dispatcher connected at ", disp)

	//daemonAddr := "127.0.0.1:30255" // Default address from daemon.go
	daemonAddr := "[fd00:f00d:cafe::7f00:a]:31010" // from 112-topo
	fmt.Print("Connecting to daemon: ", daemonAddr, " ... ")
	// TODO the following is deprecated
	daemonConn, err := daemon.NewService(daemonAddr).Connect(ctx)
	checkErr(err, "Error connecting to daemon")
	fmt.Println("done")

	revHandler := daemon.RevHandler{Connector: daemonConn}

	fmt.Print("Connection factory: ... ")
	connFactory := &snet.DefaultPacketDispatcherService{
		Dispatcher: disp,
		SCMPHandler: snet.DefaultSCMPHandler{
			RevocationHandler: revHandler,
			SCMPErrors:        scmpErrorsCounter,
		},
		SCIONPacketConnMetrics: scionPacketConnMetrics,
	}
	fmt.Println(" done")

	// register
	srcIA, err := addr.ParseIA("1-ff00:0:112")
	checkError(err)
	srcAddr, err := net.ResolveUDPAddr("udp", "127.0.0.2:0")
	checkError(err)
	fmt.Print("Registering ... ")
	conn, port, err := connFactory.Register(context.Background(), srcIA, srcAddr, addr.SvcNone)
	checkErr(err, "Error registering")
	fmt.Println(" done")

	fmt.Printf("Connected: %v,[%v]:%d \n", srcIA, srcAddr.IP, port)
	defer conn.Close()

	err = handlePing(conn)
	checkError(err)
	return 0
}

func handlePing(conn snet.PacketConn) error {
	var p snet.Packet
	var ov net.UDPAddr
	err := conn.ReadFrom(&p, &ov)
	checkErr(err, "Error reading packet")

	udp, ok := p.Payload.(snet.UDPPayload)
	checkOk(ok, "Error reading payload")

	fmt.Printf("Received message: \"%s\" from %v:%v", string(udp.Payload), ov.IP, udp.SrcPort)

	p.Destination, p.Source = p.Source, p.Destination
	p.Payload = snet.UDPPayload{
		DstPort: udp.SrcPort,
		SrcPort: udp.DstPort,
		Payload: []byte("hello again!"),
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
