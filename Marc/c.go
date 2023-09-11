package main

import (
	"context"
	"encoding/hex"
	"flag"
	"log"
	"net"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
	"github.com/scionproto/scion/pkg/snet"
)

func sendHello(daemonAddr string, localAddr snet.UDPAddr, remoteAddr snet.UDPAddr) {
	var err error
	ctx := context.Background()

	dc, err := daemon.NewService(daemonAddr).Connect(ctx)
	if err != nil {
		log.Fatal("Failed to create SCION daemon connector:", err)
	}

	ps, err := dc.Paths(ctx, remoteAddr.IA, localAddr.IA, daemon.PathReqFlags{Refresh: true})
	if err != nil {
		log.Fatal("Failed to lookup paths: %v:", err)
	}

	if len(ps) == 0 {
		log.Fatal("No paths to %v available", remoteAddr.IA)
	}

	log.Printf("Available paths to %v:\n", remoteAddr.IA)
	for _, p := range ps {
		log.Printf("\t%v\n", p)
	}

	sp := ps[0]

	log.Printf("Selected path to %v:\n", remoteAddr.IA)
	log.Printf("\t%v\n", sp)

	lconn, err := net.ListenUDP("udp", &net.UDPAddr{IP: localAddr.Host.IP})
	if err != nil {
		log.Fatalf("Failed to bind UDP connection: %v\n", err)
	}
	defer lconn.Close()

	localAddr.Host.Port = lconn.LocalAddr().(*net.UDPAddr).Port

	pkt := &snet.Packet{
		PacketInfo: snet.PacketInfo{
			Source: snet.SCIONAddress{
				IA:   localAddr.IA,
				Host: addr.HostFromIP(localAddr.Host.IP),
			},
			Destination: snet.SCIONAddress{
				IA:   remoteAddr.IA,
				Host: addr.HostFromIP(remoteAddr.Host.IP),
			},
			Path: sp.Dataplane(),
			Payload: snet.UDPPayload{
				SrcPort: uint16(localAddr.Host.Port),
				DstPort: uint16(remoteAddr.Host.Port),
				Payload: []byte("Hello, world!"),
			},
		},
	}

	nextHop := sp.UnderlayNextHop()
	if nextHop == nil && remoteAddr.IA.Equal(localAddr.IA) {
		nextHop = &net.UDPAddr{
			IP:   remoteAddr.Host.IP,
			Port: 30041, /* end host port */
			Zone: remoteAddr.Host.Zone,
		}
	}

	err = pkt.Serialize()
	if err != nil {
		log.Fatalf("Failed to serialize SCION packet: %v\n", err)
	}

	localAddr.Host.Port = 30041 /* end host port */

	dconn, err := net.ListenUDP("udp", localAddr.Host)
	if err != nil {
		log.Fatalf("Failed to bind UDP connection: %v\n", err)
	}
	defer dconn.Close()

	_, err = lconn.WriteTo(pkt.Bytes, nextHop)
	if err != nil {
		log.Fatalf("Failed to write packet: %v\n", err)
	}

	pkt.Prepare()
	n, lastHop, err := dconn.ReadFrom(pkt.Bytes)
	if err != nil {
		log.Fatalf("Failed to read packet: %v\n", err)
	}
	pkt.Bytes = pkt.Bytes[:n]

	log.Printf("[D]: received from %v\n%s", lastHop, hex.Dump(pkt.Bytes))

	err = pkt.Decode()
	if err != nil {
		log.Fatalf("Failed to decode packet: %v\n", err)
	}

	pld, ok := pkt.Payload.(snet.UDPPayload)
	if !ok {
		log.Fatalf("Failed to read packet payload\n")
	}

	m, err := dconn.WriteTo(pkt.Bytes, &net.UDPAddr{IP: pkt.Destination.Host.IP(), Port: int(pld.DstPort)})
	if err != nil || m != n {
		log.Fatalf("Failed to forward packet: %v, %v\n", err, m)
	}

	pkt.Prepare()
	n, lastHop, err = lconn.ReadFrom(pkt.Bytes)
	if err != nil {
		log.Fatalf("Failed to read packet: %v\n", err)
	}
	pkt.Bytes = pkt.Bytes[:n]

	log.Printf("[L]: received from %v\n%s", lastHop, hex.Dump(pkt.Bytes))

	err = pkt.Decode()
	if err != nil {
		log.Fatalf("Failed to decode packet: %v\n", err)
	}

	pld, ok = pkt.Payload.(snet.UDPPayload)
	if !ok {
		log.Fatalf("Failed to read packet payload\n")
	}
}

func main() {
	var daemonAddr string
	var localAddr snet.UDPAddr
	var remoteAddr snet.UDPAddr
	flag.StringVar(&daemonAddr, "daemon", "", "Daemon address")
	flag.Var(&localAddr, "local", "Local address")
	flag.Var(&remoteAddr, "remote", "Remote address")
	flag.Parse()

	sendHello(daemonAddr, localAddr, remoteAddr)
}
