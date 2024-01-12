package main

// Use tcpdump to create a test file
// tcpdump -w test.pcap
// or use the example above for writing pcap files

import (
	"fmt"
	"log"
	"os"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

func main() {
	// Open file instead of device
	handle, err := pcap.OpenOffline(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer handle.Close()

	// Set filter
	if err := handle.SetBPFFilter("tcp"); err != nil {
		log.Fatal(err)
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSource.Packets() {
		printPacketInfo(packet)
	}
}

func printPacketInfo(packet gopacket.Packet) {
	// Let's see if the packet is an ethernet packet
	ethernetLayer := packet.Layer(layers.LayerTypeEthernet)
	if ethernetLayer == nil {
		return
	}

	ethernetPacket, _ := ethernetLayer.(*layers.Ethernet)

	if ethernetPacket.EthernetType.String() == "IPv4" {
		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer == nil {
			return
		}

		ip, _ := ipLayer.(*layers.IPv4)

		// IP layer variables:
		// Version (Either 4 or 6)
		// IHL (IP Header Length in 32-bit words)
		// TOS, Length, Id, Flags, FragOffset, TTL, Protocol (TCP?),
		// Checksum, SrcIP, DstIP

		tcpLayer := packet.Layer(layers.LayerTypeTCP)
		if tcpLayer == nil {
			return
		}

		tcp, _ := tcpLayer.(*layers.TCP)

		// TCP layer variables:
		// SrcPort, DstPort, Seq, Ack, DataOffset, Window, Checksum, Urgent
		// Bool flags: FIN, SYN, RST, PSH, ACK, URG, ECE, CWR, NS
		applicationLayer := packet.ApplicationLayer()
		if applicationLayer == nil {
			return
		}

		payload := applicationLayer.Payload()
		printPacket(ethernetPacket, ip, tcp, payload)
	}
}

var seq = 0

func printPacket(packet *layers.Ethernet, ip *layers.IPv4, tcp *layers.TCP, payload []byte) {
	seq++
	// Ethernet type is typically IPv4 but could be ARP or other
	fmt.Printf("\n\n-----#%d MAC: %s->%s Ethernet: %s/%s DIR: %s:%d->%s:%d Seq: %d Flags: %s Len: %d -----\n\n",
		seq,
		packet.SrcMAC, packet.DstMAC, packet.EthernetType,
		ip.Protocol, ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort, tcp.Seq,
		tcpFlags(tcp), len(payload))

	fmt.Printf("%s", payload)
}

func tcpFlags(tcp *layers.TCP) string {
	return Join([]string{
		If(tcp.FIN, "FIN"),
		If(tcp.SYN, "SYN"),
		If(tcp.RST, "RST"),
		If(tcp.PSH, "PSH"),
		If(tcp.ACK, "ACK"),
		If(tcp.URG, "URG"),
		If(tcp.ECE, "ECE"),
		If(tcp.CWR, "CWR"),
		If(tcp.NS, "NS"),
	}, " ")
}

func Join(strs []string, sep string) string {
	joined := ""
	for _, s := range strs {
		if s == "" {
			continue
		}
		if joined == "" {
			joined = s
		} else {
			joined += sep + s
		}
	}

	return joined
}

func If(b bool, s string) string {
	if b {
		return s
	}
	return ""
}
