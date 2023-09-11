package main

import (
	"flag"
	"log"
	"net"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/snet"
)

func runServer(localAddr snet.UDPAddr) {
	var err error

	localAddr.Host.Port = 30041 /* end host port */

	log.Printf("Listening in %v on %v:%d - %v\n", localAddr.IA, localAddr.Host.IP, localAddr.Host.Port, addr.SvcNone)

	conn, err := net.ListenUDP("udp", localAddr.Host)
	if err != nil {
		log.Fatal("Failed to listen on UDP connection: %v\n", err)
	}
	defer conn.Close()

	for {
		var pkt snet.Packet
		pkt.Prepare()
		n, lastHop, err := conn.ReadFrom(pkt.Bytes)
		if err != nil {
			log.Printf("Failed to read packet: %v\n", err)
			continue
		}

		pkt.Bytes = pkt.Bytes[:n]
		err = pkt.Decode()
		if err != nil {
			log.Printf("Failed to decode packet: %v\n", err)
			continue
		}

		pld, ok := pkt.Payload.(snet.UDPPayload)
		if !ok {
			log.Printf("Failed to read packet payload\n")
			continue
		}

		log.Printf("Received payload: \"%v\"\n", string(pld.Payload))

		pkt.Destination, pkt.Source = pkt.Source, pkt.Destination
		rpath, ok := pkt.Path.(snet.RawPath)
		if !ok {
			log.Printf("Failed to reverse path, unecpected path type: %v", pkt.Path)
			continue
		}
		replypather := snet.DefaultReplyPather{}
		replyPath, err := replypather.ReplyPath(rpath)
		if err != nil {
			log.Printf("Failed to reverse path: %v", err)
			continue
		}
		pkt.Path = replyPath

		pkt.Payload = snet.UDPPayload{
			DstPort: pld.SrcPort,
			SrcPort: pld.DstPort,
			Payload: []byte("!DLROW ,OLLEh"),
		}

		err = pkt.Serialize()
		if err != nil {
			log.Printf("Failed to serialize SCION packet: %v\n", err)
			continue
		}

		_, err = conn.WriteTo(pkt.Bytes, lastHop)
		if err != nil {
			log.Printf("Failed to write packet: %v\n", err)
			continue
		}
	}
}

func main() {
	var localAddr snet.UDPAddr
	flag.Var(&localAddr, "local", "Local address")
	flag.Parse()

	runServer(localAddr)
}
