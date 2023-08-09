package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
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
	fmt.Println("Starting client ...")

	ctx := context.Background()

	fmt.Print("Connecting to dispatcher ... ")
	disp := reliable.NewDispatcher("")
	fmt.Println("done")

	daemonAddr := "[127.0.0.12]:30255" // from 110-topo
	//daemonAddr := "127.0.0.1:30255" // Default address from daemon.go
	//daemonAddr := "[fd00:f00d:cafe::7f00:a]:31010" // from 112-topo
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
	dstIA, err := addr.ParseIA("1-ff00:0:112")
	checkError(err)
	srcIA, err := addr.ParseIA("1-ff00:0:110")
	checkError(err)
	srcAddr, err := net.ResolveUDPAddr("udp", "127.0.0.2:0")
	checkError(err)
	dstAddr, err := net.ResolveUDPAddr("udp", "[::1]:8080")
	checkError(err)
	fmt.Print("Registering ... ")
	conn, port, err := connFactory.Register(context.Background(), srcIA, srcAddr, addr.SvcNone)
	checkErr(err, "Error registering")
	fmt.Println(" done")

	fmt.Printf("Connected as: %v,[%v]:%d \n", srcIA, srcAddr.IP, port)
	defer conn.Close()

	// get path
	fmt.Print("Requesting path ...")
	paths, err := daemonConn.Paths(ctx, dstIA, srcIA, daemon.PathReqFlags{}) // TODO Refresh:true?
	checkErr(err, "Error while requesting path")
	fmt.Println("done")

	fmt.Println("Path:")
	for _, pe := range paths {
		fmt.Println("   ", pe)
	}

	// TODO check that it has no error
	path := paths[0]

	// remote addr
	var remote = snet.UDPAddr{}
	remote.Host = dstAddr
	remote.Path = path.Dataplane()
	remote.NextHop = path.UnderlayNextHop()

	// send packet
	//remoteHostIP, ok := netip.AddrFromSlice(remote.Host.IP)
	//checkOk(ok, fmt.Sprintf("Failed to parse address: %v", remote.Host.IP))
	//localHostIP, ok := netip.AddrFromSlice(srcAddr.IP)
	//checkOk(ok, fmt.Sprintf("Failed to parse address: %v", srcAddr.IP))
	fmt.Printf("Destination: %v,%v\n", dstIA, dstAddr)
	fmt.Print("Creating packet ... ")
	pkt := &snet.Packet{
		PacketInfo: snet.PacketInfo{
			Destination: snet.SCIONAddress{
				IA:   dstIA,
				Host: addr.HostFromIP(dstAddr.IP),
			},
			Source: snet.SCIONAddress{
				IA:   srcIA,
				Host: addr.HostFromIP(srcAddr.IP),
			},
			Path: remote.Path,
			Payload: snet.UDPPayload{
				SrcPort: port,
				DstPort: uint16(dstAddr.Port),
				Payload: []byte("Hello scion"),
			},
		},
	}
	fmt.Println("done")

	fmt.Printf("Sending packet to first hop: %v  ... ", remote.NextHop)
	err = conn.WriteTo(pkt, remote.NextHop)
	checkErr(err, "Error while Sending packet")
	fmt.Println("done")
	return 0 // TODO
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
