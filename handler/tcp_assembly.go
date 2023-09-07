package handler

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/bingoohuang/gg/pkg/handy"
	"github.com/bingoohuang/httpdump/util"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// gopacket provide a tcp connection, however it split one tcp connection into two stream.
// So it is hard to match http request and response. we make our own connection here

const (
	maxTCPSeq    uint32 = 0xFFFFFFFF
	tcpSeqWindow        = 0x0000FFFF
)

// TCPAssembler do tcp package assemble
type TCPAssembler struct {
	lock        handy.Lock
	connections map[string]*TCPConnection
	handler     ConnectionHandler

	chanSize    uint
	processResp int
}

func NewTCPAssembler(handler ConnectionHandler, chanSize uint, processResp int) *TCPAssembler {
	return &TCPAssembler{
		connections: map[string]*TCPConnection{},
		handler:     handler,
		chanSize:    chanSize,
		processResp: processResp,
	}
}

func (r *TCPAssembler) Assemble(flow gopacket.Flow, tcp *layers.TCP, timestamp time.Time) {
	src := Endpoint{ip: flow.Src().String(), port: uint16(tcp.SrcPort)}
	dst := Endpoint{ip: flow.Dst().String(), port: uint16(tcp.DstPort)}

	key := r.createConnectionKey(src, dst)
	createNewConn := tcp.SYN && !tcp.ACK || isHTTPRequestData(tcp.Payload)
	c := r.retrieveConnection(src, dst, key, createNewConn)
	if c == nil {
		return
	}

	c.onReceive(src, tcp, timestamp)

	if c.closed() {
		r.deleteConnection(key)
		c.finish()
	}
}

func (r *TCPAssembler) createConnectionKey(src Endpoint, dst Endpoint) string {
	srcString, dstString := src.String(), dst.String()
	if srcString < dstString {
		return srcString + "-" + dstString
	}

	return dstString + "-" + srcString
}

// retrieveConnection get connection this packet belongs to; create new one if is new connection.
func (r *TCPAssembler) retrieveConnection(src, dst Endpoint, key string, init bool) *TCPConnection {
	defer r.lock.LockDeferUnlock()()

	c := r.connections[key]
	if c == nil && init {
		c = newTCPConnection(key, src, dst, r.chanSize, r.processResp)
		r.connections[key] = c
		r.handler.handle(src, dst, c)
	}
	return c
}

// deleteConnection removes connection (when is closed or timeout).
func (r *TCPAssembler) deleteConnection(key string) {
	defer r.lock.LockDeferUnlock()()
	delete(r.connections, key)
}

// FlushOlderThan flushes timeout connections.
func (r *TCPAssembler) FlushOlderThan(time time.Time) {
	var connections []*TCPConnection

	r.lock.Lock()
	for _, c := range r.connections {
		if c.lastTimestamp.Before(time) {
			connections = append(connections, c)
			delete(r.connections, c.key)
		}
	}
	r.lock.Unlock()

	for _, c := range connections {
		c.flushOlderThan()
	}
}

func (r *TCPAssembler) FinishAll() {
	defer r.lock.LockDeferUnlock()()

	for _, c := range r.connections {
		c.finish()
	}
	r.connections = nil
	r.handler.finish()
}

// TCPConnection hold info for one tcp connection
type TCPConnection struct {
	key              string
	requestStream    Stream    // stream from client to server
	responseStream   Stream    // stream from server to client
	clientID         Endpoint  // the client key(by ip and port)
	lastTimestamp    time.Time // timestamp receive last packet
	lastReqTimestamp time.Time // timestamp receive last packet
	lastRspTimestamp time.Time // timestamp receive last packet
	isHTTP           bool
}

// Endpoint is one endpoint of a tcp connection
type Endpoint struct {
	ip   string
	port uint16
}

func (p Endpoint) equals(v Endpoint) bool { return p.ip == v.ip && p.port == v.port }
func (p Endpoint) String() string         { return p.ip + ":" + strconv.Itoa(int(p.port)) }

// create tcp connection, by the first tcp packet. this packet should from client to server
func newTCPConnection(key string, src, dst Endpoint, chanSize uint, processResp int) *TCPConnection {
	t := &TCPConnection{
		key:           key,
		requestStream: newNetworkStream(src, dst, true, chanSize),
	}

	if processResp > 0 {
		t.responseStream = newNetworkStream(src, dst, false, chanSize)
	} else {
		t.responseStream = &FakeStream{}
	}

	return t
}

// when receive tcp packet
func (c *TCPConnection) onReceive(src Endpoint, tcp *layers.TCP, timestamp time.Time) {
	c.lastTimestamp = timestamp
	var (
		isReq bool
		isRsp bool
	)

	if !c.isHTTP {
		isReq = isHTTPRequestData(tcp.Payload)
		if !isReq {
			_, isRsp = util.ParseResponseTitle(tcp.Payload)
		}
		if !isReq && !isRsp {
			return // skip no-http data
		}
		// receive first valid http data packet
		c.clientID = src
		c.isHTTP = true
	}

	var send, confirm Stream
	if c.clientID.equals(src) {
		send = c.requestStream
		confirm = c.responseStream
	} else {
		send = c.responseStream
		confirm = c.requestStream
		c.lastRspTimestamp = c.lastTimestamp
	}

	if isReq {
		c.lastReqTimestamp = c.lastTimestamp
	} else {
		c.lastRspTimestamp = c.lastTimestamp
	}

	send.AppendPacket(tcp)

	// if tcp.SYN { /* do nothing*/ }

	if tcp.ACK { // confirm
		confirm.ConfirmPacket(tcp.Ack)
	}

	// terminate c
	if tcp.FIN || tcp.RST {
		send.SetClosed(true)
	}
}

// just close this connection?
func (c *TCPConnection) flushOlderThan() {
	// flush all data
	// c.requestStream.window
	// c.responseStream.window
	// remove and close c
	c.requestStream.SetClosed(true)
	c.responseStream.SetClosed(true)
	c.finish()
}

func (c *TCPConnection) closed() bool {
	return c.requestStream.IsClosed() && c.responseStream.IsClosed()
}

func (c *TCPConnection) finish() {
	c.requestStream.Finish()
	c.responseStream.Finish()
}

// NetworkStream tread one-direction tcp data as stream. impl reader closer
type NetworkStream struct {
	window *ReceiveWindow
	c      chan *layers.TCP
	ignore bool
	closed bool

	src, dst  Endpoint
	isRequest bool
}

func (s *NetworkStream) SetClosed(closed bool) { s.closed = closed }
func (s *NetworkStream) IsClosed() bool        { return s.closed }

type Stream interface {
	AppendPacket(tcp *layers.TCP)
	ConfirmPacket(ack uint32)
	SetClosed(closed bool)
	IsClosed() bool
	Finish()
	Packets() chan *layers.TCP
	Close() error
	DiscardAll()
}

type FakeStream struct {
	closed bool
}

func (*FakeStream) GetLastUUID() []byte         { panic("should not be called") }
func (*FakeStream) Close() error                { panic("should not be called") }
func (f *FakeStream) Packets() chan *layers.TCP { panic("should not be called") }
func (*FakeStream) AppendPacket(*layers.TCP)    {}
func (*FakeStream) ConfirmPacket(uint32)        {}
func (f *FakeStream) SetClosed(closed bool)     { f.closed = closed }
func (f *FakeStream) IsClosed() bool            { return f.closed }
func (*FakeStream) Finish()                     {}
func (*FakeStream) DiscardAll()                 {}

func newNetworkStream(src, dst Endpoint, isRequest bool, chanSize uint) Stream {
	return &NetworkStream{
		window:    newReceiveWindow(64),
		c:         make(chan *layers.TCP, chanSize),
		src:       src,
		dst:       dst,
		isRequest: isRequest,
	}
}

func (s *NetworkStream) AppendPacket(tcp *layers.TCP) {
	if s.ignore {
		return
	}
	s.window.insert(tcp)
}

func (s *NetworkStream) ConfirmPacket(ack uint32) {
	if s.ignore {
		return
	}
	s.window.confirm(ack, s.c)
}

func (s *NetworkStream) Finish() { close(s.c) }

// UUID returns the UUID of a TCP request and its response.
func (s NetworkStream) UUID(p *layers.TCP) []byte {
	l, r := s.src, s.dst
	streamID := uint64(l.port)<<48 | uint64(r.port)<<32 | uint64(ip2int(l.ip))
	id := make([]byte, 12)
	binary.BigEndian.PutUint64(id, streamID)

	if s.isRequest {
		binary.BigEndian.PutUint32(id[8:], p.Ack)
	} else {
		binary.BigEndian.PutUint32(id[8:], p.Seq)
	}

	uid := make([]byte, 24)
	hex.Encode(uid[:], id[:])

	return []byte(fmt.Sprintf("id:%s,Seq:%d,Ack:%d", uid, p.Seq, p.Ack))
}

func ip2int(v string) uint32 {
	ip := net.ParseIP(v)
	if len(ip) == 0 {
		return 0
	}

	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func (s *NetworkStream) Packets() chan *layers.TCP { return s.c }

func (s *NetworkStream) DiscardAll() {
	for range s.c {
	}
}

// Close the stream
func (s *NetworkStream) Close() error {
	s.ignore = true
	return nil
}

// ReceiveWindow simulate tcp receivec window
type ReceiveWindow struct {
	size        int
	start       int
	buffer      []*layers.TCP
	lastAck     uint32
	expectBegin uint32
}

func newReceiveWindow(initialSize int) *ReceiveWindow {
	buffer := make([]*layers.TCP, initialSize)
	return &ReceiveWindow{buffer: buffer}
}

func (w *ReceiveWindow) destroy() {
	w.size = 0
	w.start = 0
	w.buffer = nil
}

func (w *ReceiveWindow) insert(packet *layers.TCP) {
	if len(packet.Payload) == 0 {
		return // ignore empty data packet
	}

	if w.expectBegin != 0 && compareTCPSeq(w.expectBegin, packet.Seq+uint32(len(packet.Payload))) >= 0 {
		return // dropped
	}

	idx := w.size
	for ; idx > 0; idx-- {
		index := (idx - 1 + w.start) % len(w.buffer)
		prev := w.buffer[index]
		result := compareTCPSeq(prev.Seq, packet.Seq)
		if result == 0 { // duplicated
			return
		}
		if result < 0 { // insert at index
			break
		}
	}

	if w.size == len(w.buffer) {
		w.expand()
	}

	if idx == w.size { // append at last
		index := (idx + w.start) % len(w.buffer)
		w.buffer[index] = packet
	} else { // insert at index
		for i := w.size - 1; i >= idx; i-- {
			next := (i + w.start + 1) % len(w.buffer)
			current := (i + w.start) % len(w.buffer)
			w.buffer[next] = w.buffer[current]
		}
		index := (idx + w.start) % len(w.buffer)
		w.buffer[index] = packet
	}

	w.size++
}

// send confirmed packets to reader, when receive ack
func (w *ReceiveWindow) confirm(ack uint32, c chan *layers.TCP) {
	idx := 0
	for ; idx < w.size; idx++ {
		index := (idx + w.start) % len(w.buffer)
		packet := w.buffer[index]
		if result := compareTCPSeq(packet.Seq, ack); result >= 0 {
			break
		}
		w.buffer[index] = nil
		newExpect := packet.Seq + uint32(len(packet.Payload))
		if w.expectBegin != 0 {
			diff := compareTCPSeq(w.expectBegin, packet.Seq)
			if diff > 0 {
				duplicatedSize := w.expectBegin - packet.Seq
				if duplicatedSize < 0 {
					duplicatedSize += maxTCPSeq
				}
				if duplicatedSize >= uint32(len(packet.Payload)) {
					continue
				}
				packet.Payload = packet.Payload[duplicatedSize:]
			} else if diff < 0 {
				// TODO: we lose packet here
			}
		}
		c <- packet
		w.expectBegin = newExpect
	}
	w.start = (w.start + idx) % len(w.buffer)
	w.size = w.size - idx
	if compareTCPSeq(w.lastAck, ack) < 0 || w.lastAck == 0 {
		w.lastAck = ack
	}
}

func (w *ReceiveWindow) expand() {
	buffer := make([]*layers.TCP, len(w.buffer)*2)
	end := w.start + w.size
	if end < len(w.buffer) {
		copy(buffer, w.buffer[w.start:w.start+w.size])
	} else {
		copy(buffer, w.buffer[w.start:])
		copy(buffer[len(w.buffer)-w.start:], w.buffer[:end-len(w.buffer)])
	}
	w.start = 0
	w.buffer = buffer
}

// compare two tcp sequences, if seq1 is earlier, return num < 0, if seq1 == seq2, return 0, else return num > 0
func compareTCPSeq(seq1, seq2 uint32) int {
	if seq1 < tcpSeqWindow && seq2 > maxTCPSeq-tcpSeqWindow {
		return int(int32(seq1 + maxTCPSeq - seq2))
	} else if seq2 < tcpSeqWindow && seq1 > maxTCPSeq-tcpSeqWindow {
		return int(int32(seq1 - (maxTCPSeq + seq2)))
	}
	return int(int32(seq1 - seq2))
}

var httpMethods = map[string]bool{
	"CONNECT": true,
	"GET":     true,
	"POST":    true,
	"PUT":     true,
	"DELETE":  true,
	"HEAD":    true,
	"TRACE":   true,
	"OPTIONS": true,
	"PATCH":   true,
}

// if is first http request packet
func isHTTPRequestData(body []byte) bool {
	if len(body) < 8 {
		return false
	}
	data := body[:8]
	idx := bytes.IndexByte(data, byte(' '))
	if idx < 0 {
		return false
	}

	method := string(data[:idx])
	return httpMethods[method]
}
