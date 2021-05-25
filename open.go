package main

import (
	"errors"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"log"
	"net"
	"os"
	"strconv"
)

func createPacketsChan(input, bpf, host, ip string, port uint) (chan gopacket.Packet, error) {
	if v, err := os.Stat(input); err == nil && !v.IsDir() {
		var handle, err = pcap.OpenOffline(input) // read from pcap file
		if err != nil {
			return nil, fmt.Errorf("open file %v error: %w", input, err)
		}
		if err = setDeviceFilter(handle, bpf, ip, uint16(port)); err != nil {
			return nil, fmt.Errorf("set filter %v error: %w", input, err)
		}

		return listenOneSource(handle), nil
	}

	if input == "any" && host != "" {
		// capture all device
		// Only linux 2.2+ support any interface. we have to list all network device and listened on them all
		interfaces, err := ListInterfaces(host)
		if err != nil {
			return nil, fmt.Errorf("find device error: %w", err)
		}

		var packetsSlice = make([]chan gopacket.Packet, len(interfaces))
		for _, itf := range interfaces {
			localPackets, err := OpenSingleDevice(itf.Name, bpf, ip, uint16(port))
			if err != nil {
				fmt.Fprintln(os.Stderr, "open device", itf, "error:", err)
				continue
			}
			log.Printf("open deive %s", itf.Name)
			packetsSlice = append(packetsSlice, localPackets)
		}
		if len(packetsSlice) == 0 {
			return nil, fmt.Errorf("no device available")
		}

		return mergeChannel(packetsSlice), nil
	}

	// capture one device
	return OpenSingleDevice(input, bpf, ip, uint16(port))
}

func OpenSingleDevice(device, bpf, filterIP string, filterPort uint16) (localPackets chan gopacket.Packet, err error) {
	defer func() {
		if msg := recover(); msg != nil {
			switch x := msg.(type) {
			case string:
				err = errors.New(x)
			case error:
				err = x
			default:
				err = fmt.Errorf("%v", msg)
			}
			localPackets = nil
		}
	}()
	handle, err := pcap.OpenLive(device, 65536, false, pcap.BlockForever)
	if err != nil {
		return
	}

	if err = setDeviceFilter(handle, bpf, filterIP, filterPort); err != nil {
		return
	}
	localPackets = listenOneSource(handle)
	return
}

func listenOneSource(handle *pcap.Handle) chan gopacket.Packet {
	ps := gopacket.NewPacketSource(handle, handle.LinkType())
	return ps.Packets()
}

// set packet capture filter, by ip and port
func setDeviceFilter(handle *pcap.Handle, bpf, filterIP string, filterPort uint16) error {
	if bpf != "" {
		return handle.SetBPFFilter(bpf)
	}

	var bpfFilter = "tcp"
	if filterPort != 0 {
		bpfFilter += " port " + strconv.Itoa(int(filterPort))
	}
	if filterIP != "" {
		bpfFilter += " ip host " + filterIP
	}
	return handle.SetBPFFilter(bpfFilter)
}

func ListInterfaces(host string) (ifacesHasAddr []net.Interface, err error) {
	var ifis []net.Interface
	ifis, err = net.Interfaces()
	if err != nil {
		return
	}

	ifacesHasAddr = make([]net.Interface, 0, len(ifis))

	for _, ifi := range ifis {
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, e := ifi.Addrs()
		if e != nil {
			// don't give up on a failure from a single interface
			continue
		}

		if len(addrs) > 0 {
			ifacesHasAddr = append(ifacesHasAddr, ifi)
		}

		for _, addr := range addrs {
			if cutMask(addr) == host {
				return []net.Interface{ifi}, nil
			}
		}
	}
	return
}

func cutMask(addr net.Addr) string {
	mask := addr.String()
	for i, v := range mask {
		if v == '/' {
			return mask[:i]
		}
	}
	return mask
}

// adapter multi channels to one channel. used to aggregate multi devices data
func mergeChannel(channels []chan gopacket.Packet) chan gopacket.Packet {
	var channel = make(chan gopacket.Packet)
	for _, ch := range channels {
		go func(c chan gopacket.Packet) {
			for packet := range c {
				channel <- packet
			}
		}(ch)
	}
	return channel
}
