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

// 打印 pcap 中的 TCP 包, pcap 文件采集示例: `tcpdump -i any -s0 port 9200 -C1 -w 9200.pcap`
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

	packetSrc := gopacket.NewPacketSource(handle, handle.LinkType())
	for packet := range packetSrc.Packets() {
		printPacketInfo(packet)
	}
}

func printPacketInfo(p gopacket.Packet) {
	// only assembly tcp/ip packets
	n, t := p.NetworkLayer(), p.TransportLayer()
	if n == nil || t == nil || t.LayerType() != layers.LayerTypeTCP {
		return
	}

	// IP layer variables:
	// Version (Either 4 or 6)
	// IHL (IP Header Length in 32-bit words)
	// TOS, Length, Id, Flags, FragOffset, TTL, Protocol (TCP?),
	// Checksum, SrcIP, DstIP

	// TCP layer variables:
	// SrcPort, DstPort, Seq, Ack, DataOffset, Window, Checksum, Urgent
	// Bool flags: FIN, SYN, RST, PSH, ACK, URG, ECE, CWR, NS

	var payload []byte
	if l := p.ApplicationLayer(); l != nil {
		payload = l.Payload()
	}

	printPacket(n.NetworkFlow(), t.(*layers.TCP), payload)
}

var seq = 0

func printPacket(flow gopacket.Flow, tcp *layers.TCP, payload []byte) {
	seq++
	fmt.Printf("\n\n-----#%d DIR: %s:%d->%s:%d Seq: %d Flags: %s Len: %d -----\n\n",
		seq,
		flow.Src(), tcp.SrcPort, flow.Dst(), tcp.DstPort, tcp.Seq,
		tcpFlags(tcp), len(payload))

	if len(payload) > 0 {
		fmt.Printf("%s", payload)
	}
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

func Join(strs []string, sep string) (joined string) {
	for _, s := range strs {
		if s != "" {
			joined += If(joined != "", sep) + s
		}
	}

	return joined
}

func Iff[T any](b bool, s1, s2 T) T {
	if b {
		return s1
	}
	return s2
}

func If[T any](b bool, s T) T {
	if b {
		return s
	}
	var t T
	return t
}
