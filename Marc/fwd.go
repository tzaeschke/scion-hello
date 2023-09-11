package main

import (
	"flag"
	"log"
	"net"
	"net/netip"

	"github.com/google/gopacket"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/slayers"
	"github.com/scionproto/scion/pkg/snet"
)

// A minimal "Dispatcher" implementation
func runFwd(localAddr snet.UDPAddr) {
	var err error

	localAddr.Host.Port = 40041 /* end host port */

	log.Printf("Listening in %v on %v:%d - %v\n", localAddr.IA, localAddr.Host.IP, localAddr.Host.Port, addr.SvcNone)

	conn, err := net.ListenUDP("udp", localAddr.Host)
	if err != nil {
		log.Fatal("Failed to listen on UDP connection: %v\n", err)
	}
	defer conn.Close()

	buf := make([]byte, 9216-20-8 /* MTU supported by SCION */)

	var (
		scionLayer slayers.SCION
		hbhLayer   slayers.HopByHopExtnSkipper
		e2eLayer   slayers.EndToEndExtn
		udpLayer   slayers.UDP
		scmpLayer  slayers.SCMP
	)
	scionLayer.RecyclePaths()
	udpLayer.SetNetworkLayerForChecksum(&scionLayer)
	scmpLayer.SetNetworkLayerForChecksum(&scionLayer)
	parser := gopacket.NewDecodingLayerParser(
		slayers.LayerTypeSCION, &scionLayer, &hbhLayer, &e2eLayer, &udpLayer, &scmpLayer,
	)
	parser.IgnoreUnsupported = true
	decoded := make([]gopacket.LayerType, 4)
	buffer := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}

	for {
		buf = buf[:cap(buf)]

		n, _, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			log.Printf("failed to read packet: %v\n", err)
			continue
		}
		buf = buf[:n]

		err = parser.DecodeLayers(buf, &decoded)
		if err != nil {
			log.Printf("failed to decode packet: %v\n", err)
			continue
		}
		validType := len(decoded) >= 2 &&
			decoded[len(decoded)-1] == slayers.LayerTypeSCIONUDP
		if !validType {
			log.Printf("failed to decode packet: unexpected type or structure\n")
			continue
		}

		if int(udpLayer.DstPort) != localAddr.Host.Port {
			dstAddr, ok := netip.AddrFromSlice(scionLayer.RawDstAddr)
			if !ok {
				panic("unexpected IP address byte slice")
			}
			dstAddrPort := netip.AddrPortFrom(dstAddr, udpLayer.DstPort)
			payload := gopacket.Payload(udpLayer.Payload)

			err = buffer.Clear()
			if err != nil {
				panic(err)
			}

			err = payload.SerializeTo(buffer, options)
			if err != nil {
				panic(err)
			}
			buffer.PushLayer(payload.LayerType())

			err = udpLayer.SerializeTo(buffer, options)
			if err != nil {
				panic(err)
			}
			buffer.PushLayer(udpLayer.LayerType())

			if scionLayer.NextHdr == slayers.End2EndClass {
				err = e2eLayer.SerializeTo(buffer, options)
				if err != nil {
					panic(err)
				}
				buffer.PushLayer(e2eLayer.LayerType())
			}

			err = scionLayer.SerializeTo(buffer, options)
			if err != nil {
				panic(err)
			}
			buffer.PushLayer(scionLayer.LayerType())

			m, err := conn.WriteToUDPAddrPort(buffer.Bytes(), dstAddrPort)
			if err != nil || m != len(buffer.Bytes()) {
				log.Printf("failed to write packet: %v\n", err)
				continue
			}
		}
	}
}

func main() {
	var localAddr snet.UDPAddr
	flag.Var(&localAddr, "local", "Local address")
	flag.Parse()

	runFwd(localAddr)
}
