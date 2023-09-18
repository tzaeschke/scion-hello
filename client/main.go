package main

import (
	"context"
	"fmt"
	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
	"github.com/scionproto/scion/pkg/snet"
	"github.com/scionproto/scion/pkg/snet/metrics"
	"github.com/scionproto/scion/pkg/sock/reliable"
	"net"
	"os"
)

var (
	scionPacketConnMetrics = metrics.NewSCIONPacketConnMetrics()
	scmpErrorsCounter      = scionPacketConnMetrics.SCMPErrors
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
	fmt.Println("src=", srcIA)
	fmt.Println("dst=", dstIA)
	srcAddr, err := net.ResolveUDPAddr("udp", "127.0.0.2:100")
	checkError(err)
	dstAddr, err := net.ResolveUDPAddr("udp", "[::1]:8080")
	//dstAddr, err := net.ResolveUDPAddr("udp", "[127.0.0.111]:8080")
	checkError(err)
	fmt.Print("Registering ... ")
	conn, port, err := connFactory.Register(context.Background(), srcIA, srcAddr, addr.SvcNone)
	checkErr(err, "Error registering")
	defer conn.Close()
	fmt.Println(" done")
	fmt.Printf("Connected as: %v,[%v]:%d \n", srcIA, srcAddr.IP, port)

	// get path
	fmt.Print("Requesting path ...")
	paths, err := daemonConn.Paths(ctx, dstIA, srcIA, daemon.PathReqFlags{}) // TODO Refresh:true?
	checkErr(err, "Error while requesting path")
	fmt.Println("done")

	fmt.Println("Path:")
	for _, pe := range paths {
		fmt.Println("   ", pe)
		fmt.Println("        underlay=", pe.UnderlayNextHop())
		//fmt.Println("        plane", pe.Dataplane().)
		fmt.Println("        src=", pe.Source())
		fmt.Println("        dst=", pe.Destination())
		meta := pe.Metadata()
		fmt.Println("        meta=", pe.Metadata())
		fmt.Println("            Interfaces=", meta.Interfaces)
		fmt.Println("            Geo=", meta.Geo)
		fmt.Println("            Bandwidth=", meta.Bandwidth)
		fmt.Println("            EpicAuths=", meta.EpicAuths)
		fmt.Println("            Expiry=", meta.Expiry)
		fmt.Println("            InternalHops=", meta.InternalHops)
		fmt.Println("            Latency=", meta.Latency)
		fmt.Println("            LinkType=", meta.LinkType)
		fmt.Println("            MTU=", meta.MTU)
		fmt.Println("            Notes=", meta.Notes)
	}

	if len(paths) == 0 {
		fmt.Println("  ERROR: No paths found. Try running `./scion.sh topology -c topology/tiny.topo` first.")
		fmt.Println("         Also make sure that `./scion.sh run` is executed in a (venv).")
		return 1
	}

	// send packet
	sendPacket(conn, dstIA, dstAddr, srcIA, srcAddr, port, paths)

	// receive answer
	receiveAnswer(conn)

	// send packet
	sendPacket(conn, dstIA, dstAddr, srcIA, srcAddr, port, paths)

	// receive answer
	receiveAnswer(conn)

	return 0 // TODO
}

func sendPacket(conn snet.PacketConn, dstIA addr.IA, dstAddr *net.UDPAddr, srcIA addr.IA, srcAddr *net.UDPAddr, returnPort uint16, paths []snet.Path) {
	fmt.Printf("Source: %v,%v\n", srcIA, srcAddr)
	fmt.Printf("Destination: %v,%v\n", dstIA, dstAddr)
	fmt.Print("Creating packet ... ")
	path := paths[0]
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
			Path: path.Dataplane(),
			Payload: snet.UDPPayload{
				SrcPort: returnPort,
				DstPort: uint16(dstAddr.Port),
				Payload: []byte("Hello scion"),
			},
		},
	}
	fmt.Println("done")
	fmt.Println("pkt bytes: ", pkt.Bytes)

	fmt.Printf("Sending packet to first hop: %v  ... ", path.UnderlayNextHop())
	err := conn.WriteTo(pkt, path.UnderlayNextHop())
	fmt.Println("pkt bytes: ", pkt.Bytes)
	checkErr(err, "Error while Sending packet")
	fmt.Println("done")
}

func receiveAnswer(conn snet.PacketConn) error {
	var p snet.Packet
	var ov net.UDPAddr
	fmt.Print("Waiting ... ")
	err := conn.ReadFrom(&p, &ov)
	checkErr(err, "Error reading packet")
	fmt.Println("received answer")

	udp, ok := p.Payload.(snet.UDPPayload)
	checkOk(ok, "Error reading payload")

	fmt.Printf("Received message: \"%s\" from %v:%v\n", string(udp.Payload), ov.IP, udp.SrcPort)

	//p.Destination, p.Source = p.Source, p.Destination
	//p.Payload = snet.UDPPayload{
	//	DstPort: udp.SrcPort,
	//	SrcPort: udp.DstPort,
	//	Payload: []byte("Re: " + string(udp.Payload)),
	//}
	//
	//// reverse path
	//rpath, ok := p.Path.(snet.RawPath)
	//if !ok {
	//	return serrors.New("unecpected path", "type", common.TypeOf(p.Path))
	//}
	//
	//replypather := snet.DefaultReplyPather{}
	//replyPath, err := replypather.ReplyPath(rpath)
	//if err != nil {
	//	return serrors.WrapStr("creating reply path", err)
	//}
	//p.Path = replyPath
	//// Send pong
	//if err := conn.WriteTo(&p, &ov); err != nil {
	//	return serrors.WrapStr("sending reply", err)
	//}
	//
	//fmt.Println("Sent answer to:", p.Destination)
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
