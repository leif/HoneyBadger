package HoneyBadger

import (
	"bytes"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"container/ring"
	//	"fmt"
	"github.com/david415/HoneyBadger/types"
	"log"
	"net"
	"testing"
)

type reassemblyInput struct {
	Seq     uint32
	Payload []byte
}

type TestOverlapBytesWant struct {
	bytes       []byte
	startOffset int
	endOffset   int
}

type DummyAttackLogger struct {
	Count int
}

func NewDummyAttackLogger() *DummyAttackLogger {
	a := DummyAttackLogger{
		Count: 0,
	}
	return &a
}

func (d *DummyAttackLogger) Log(event *types.Event) {
	d.Count += 1
}

func TestInjectionDetector(t *testing.T) {
	log.Print("TestInjectionDetector")
	attackLogger := NewDummyAttackLogger()
	options := ConnectionOptions{
		MaxBufferedPagesTotal:         0,
		MaxBufferedPagesPerConnection: 0,
		MaxRingPackets:                40,
		CloseRequestChan:              nil,
		Pager:                         nil,
		LogDir:                        "fake-log-dir",
	}
	conn := NewConnection(&options)
	conn.ClientStreamRing.Value = types.Reassembly{
		Seq:   types.Sequence(5),
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	conn.ClientStreamRing = conn.ClientStreamRing.Next()
	conn.AttackLogger = attackLogger
	p := PacketManifest{
		IP: layers.IPv4{
			SrcIP:    net.IP{1, 2, 3, 4},
			DstIP:    net.IP{2, 3, 4, 5},
			Version:  4,
			TTL:      64,
			Protocol: layers.IPProtocolTCP,
		},
		TCP: layers.TCP{
			Seq:     7,
			SrcPort: 1,
			DstPort: 2,
		},
		Payload: []byte{1, 2, 3, 4, 5, 6, 7},
	}

	ipFlow, _ := gopacket.FlowFromEndpoints(layers.NewIPEndpoint(net.IPv4(1, 2, 3, 4)), layers.NewIPEndpoint(net.IPv4(2, 3, 4, 5)))
	tcpFlow, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(layers.TCPPort(1)), layers.NewTCPPortEndpoint(layers.TCPPort(2)))
	conn.serverFlow = types.NewTcpIpFlowFromFlows(ipFlow, tcpFlow)
	conn.clientFlow = conn.serverFlow.Reverse()
	conn.detectInjection(p, conn.serverFlow)

	if attackLogger.Count == 0 {
		t.Errorf("detectInjection failed; count == %d\n", attackLogger.Count)
		t.Fail()
	}

	// next test case
	p.TCP = layers.TCP{
		Seq:     7,
		SrcPort: 1,
		DstPort: 2,
	}
	p.Payload = []byte{3, 4, 5}
	conn.detectInjection(p, conn.serverFlow)
	if attackLogger.Count == 0 {
		t.Error("failed to detect injection\n")
		t.Fail()
	}

	// next test case
	attackLogger.Count = 0
	p.TCP = layers.TCP{
		Seq:     1,
		SrcPort: 1,
		DstPort: 2,
	}
	p.Payload = []byte{1, 2, 3, 4, 5, 6}
	conn.detectInjection(p, conn.serverFlow)
	if attackLogger.Count == 0 {
		t.Error("failed to detect injection\n")
		t.Fail()
	}

	// next test case
	attackLogger.Count = 0
	p.TCP = layers.TCP{
		Seq:     1,
		SrcPort: 1,
		DstPort: 2,
	}
	p.Payload = []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17}
	conn.detectInjection(p, conn.serverFlow)
	if attackLogger.Count != 1 {
		t.Error("injection detection failure\n")
		t.Fail()
	}
}

func TestGetRingSlice(t *testing.T) {
	options := ConnectionOptions{
		MaxBufferedPagesTotal:         0,
		MaxBufferedPagesPerConnection: 0,
		MaxRingPackets:                40,
		CloseRequestChan:              nil,
		Pager:                         nil,
		LogDir:                        "fake-log-dir",
	}
	conn := NewConnection(&options)
	for j := 5; j < 40; j += 5 {
		conn.ClientStreamRing.Value = types.Reassembly{
			Seq:   types.Sequence(j),
			Bytes: []byte{1, 2, 3, 4, 5},
		}
		conn.ClientStreamRing = conn.ClientStreamRing.Next()
	}
	var startSeq uint32 = 5
	p := PacketManifest{
		IP: layers.IPv4{
			SrcIP:    net.IP{1, 2, 3, 4},
			DstIP:    net.IP{2, 3, 4, 5},
			Version:  4,
			TTL:      64,
			Protocol: layers.IPProtocolTCP,
		},
		TCP: layers.TCP{
			Seq:     startSeq,
			SrcPort: 1,
			DstPort: 2,
		},
		Payload: []byte{1, 2, 3, 4, 5, 6, 7},
	}

	ipFlow, _ := gopacket.FlowFromEndpoints(layers.NewIPEndpoint(net.IPv4(1, 2, 3, 4)), layers.NewIPEndpoint(net.IPv4(2, 3, 4, 5)))
	tcpFlow, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(layers.TCPPort(1)), layers.NewTCPPortEndpoint(layers.TCPPort(2)))
	conn.serverFlow = types.NewTcpIpFlowFromFlows(ipFlow, tcpFlow)
	conn.clientFlow = conn.serverFlow.Reverse()

	head, tail := getOverlapRings(p, conn.serverFlow, conn.ClientStreamRing)

	ringSlice := getRingSlice(head, tail, 0, 1)
	if !bytes.Equal(ringSlice, []byte{1, 2, 3, 4, 5, 1}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 0, 3)
	if !bytes.Equal(ringSlice, []byte{1, 2, 3, 4, 5, 1, 2, 3}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 0, 1)
	if !bytes.Equal(ringSlice, []byte{1, 2, 3, 4, 5, 1}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 1, 0)
	if !bytes.Equal(ringSlice, []byte{2, 3, 4, 5}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 1, 1)
	if !bytes.Equal(ringSlice, []byte{2, 3, 4, 5, 1}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 2, 0)
	if !bytes.Equal(ringSlice, []byte{3, 4, 5}) {
		t.Error("byte comparison failed")
		t.Fail()
	}
	ringSlice = getRingSlice(head, tail, 2, 3)
	if !bytes.Equal(ringSlice, []byte{3, 4, 5, 1, 2, 3}) {
		t.Error("byte comparison failed")
		t.Fail()
	}

	startSeq = 1
	p = PacketManifest{
		IP: layers.IPv4{
			SrcIP:    net.IP{1, 2, 3, 4},
			DstIP:    net.IP{2, 3, 4, 5},
			Version:  4,
			TTL:      64,
			Protocol: layers.IPProtocolTCP,
		},
		TCP: layers.TCP{
			Seq:     startSeq,
			SrcPort: 1,
			DstPort: 2,
		},
		Payload: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
	}

	head, tail = getOverlapRings(p, conn.serverFlow, conn.ClientStreamRing)
	ringSlice = getRingSlice(head, tail, 0, 2)

	if !bytes.Equal(ringSlice, []byte{1, 2, 3, 4, 5, 1, 2}) {
		t.Errorf("ringSlice is %x\n", ringSlice)
		t.Fail()
	}

	ringSlice = getRingSlice(head, tail, 2, 4)
	if !bytes.Equal(ringSlice, []byte{3, 4, 5, 1, 2, 3, 4}) {
		t.Errorf("ringSlice is %x\n", ringSlice) //XXX
		t.Fail()
	}
}

func TestGetRingSlicePanic1(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("failed to panic")
			t.Fail()
		}
	}()

	head := ring.New(3)
	head.Value = types.Reassembly{
		Seq:   types.Sequence(2),
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	_ = getRingSlice(head, nil, 6, 0)
}

func TestGetRingSlicePanic3(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("failed to panic")
			t.Fail()
		}
	}()

	head := ring.New(3)
	head.Value = types.Reassembly{
		Seq:   types.Sequence(2),
		Bytes: []byte{1, 2, 3, 4, 5},
	}

	tail := ring.New(3)
	tail.Value = types.Reassembly{
		Seq:   types.Sequence(2),
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	_ = getRingSlice(head, tail, 0, 6)
}

func TestGetRingSlicePanic4(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("failed to panic")
			t.Fail()
		}
	}()

	head := ring.New(3)
	head.Value = types.Reassembly{
		Seq:   types.Sequence(2),
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	_ = getRingSlice(head, head, 0, 0)
}

func TestGetEndSequence(t *testing.T) {
	var tail *ring.Ring = ring.New(10)
	var end types.Sequence

	end = 9
	tail.Value = types.Reassembly{
		Seq:   5,
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	endSeq := getEndSequence(tail, end)
	if endSeq.Difference(end) != 0 {
		t.Errorf("endSeq %d != end %d\n", endSeq, end)
		t.Fail()
	}

	end = 9
	tail.Value = types.Reassembly{
		Seq:   5,
		Bytes: []byte{1, 2, 3, 4, 5},
	}
	endSeq = getEndSequence(tail, end.Add(1))
	if endSeq.Difference(end) != 0 {
		t.Errorf("endSeq %d != end %d\n", endSeq, end)
		t.Fail()
	}
}

func TestGetStartSequence(t *testing.T) {
	var start types.Sequence = 4
	var head *ring.Ring = ring.New(10)
	head.Value = types.Reassembly{
		Seq:   3,
		Bytes: []byte{1, 2, 3, 4, 5, 6, 7},
	}
	startSeq := getStartSequence(head, start)
	if startSeq.Difference(start) != 0 {
		t.Errorf("startSeq %d != start %d\n", startSeq, start)
		t.Fail()
	}

	start = 2
	startSeq = getStartSequence(head, start)
	if startSeq != start.Add(1) {
		t.Errorf("startSeq %d != start %d\n", startSeq, start.Add(1))
		t.Fail()
	}
}

func TestGetHeadRingOffset(t *testing.T) {
	head := ring.New(3)
	head.Value = types.Reassembly{
		Seq:   3,
		Bytes: []byte{1, 2, 3, 4, 5, 6, 7},
	}
	offset := getHeadRingOffset(head, 5)
	if offset < 0 {
		t.Error("offset less than zero\n")
		t.Fail()
	}
	if offset != 2 {
		t.Error("offset incorrect\n")
		t.Fail()
	}
	offset = getHeadRingOffset(head, 3)
	if offset != 0 {
		t.Error("offset incorrect\n")
		t.Fail()
	}

	offset = getHeadRingOffset(head, 4)
	if offset != 1 {
		t.Error("offset incorrect\n")
		t.Fail()
	}
}

func TestGetTailRingOffset(t *testing.T) {
	tail := ring.New(3)
	tail.Value = types.Reassembly{
		Seq:   3,
		Bytes: []byte{1, 2, 3, 4, 5, 6, 7},
	}

	offset := getTailRingOffset(tail, 4)
	if offset != 5 {
		t.Errorf("want 5 got %d\n", offset)
		t.Fail()
	}

	offset = getTailRingOffset(tail, 5)
	if offset != 4 {
		t.Errorf("want 4 got %d\n", offset)
		t.Fail()
	}

	offset = getTailRingOffset(tail, 6)
	if offset != 3 {
		t.Errorf("want 3 got %d\n", offset)
		t.Fail()
	}
}

func TestGetStartOverlapSequenceAndOffset(t *testing.T) {
	var start types.Sequence = 3
	head := ring.New(3)
	head.Value = types.Reassembly{
		Seq:   3,
		Bytes: []byte{1, 2, 3, 4, 5, 6, 7},
	}
	sequence, offset := getStartOverlapSequenceAndOffset(head, start)
	if offset != 0 {
		t.Error("offset != 0\n")
		t.Fail()
	}
	if sequence != 3 {
		t.Error("incorrect sequence")
		t.Fail()
	}

	start = 4
	sequence, offset = getStartOverlapSequenceAndOffset(head, start)
	if offset != 0 {
		t.Errorf("offset %d != 1\n", offset)
		t.Fail()
	}
	if sequence != 4 {
		t.Error("incorrect sequence")
		t.Fail()
	}

	start = 2
	sequence, offset = getStartOverlapSequenceAndOffset(head, start)
	if offset != 1 {
		t.Errorf("offset %d != 1\n", offset)
		t.Fail()
	}
	if sequence != 3 {
		t.Error("incorrect sequence")
		t.Fail()
	}

	start = 1
	sequence, offset = getStartOverlapSequenceAndOffset(head, start)
	if offset != 2 {
		t.Errorf("offset %d != 2\n", offset)
		t.Fail()
	}
	if sequence != 3 {
		t.Error("incorrect sequence")
		t.Fail()
	}
}

func TestGetEndOverlapSequenceAndOffset(t *testing.T) {
	var end types.Sequence = 3
	tail := ring.New(3)
	tail.Value = types.Reassembly{
		Seq:   3,
		Bytes: []byte{1, 2, 3, 4, 5, 6, 7},
	}
	sequence, offset := getEndOverlapSequenceAndOffset(tail, end)
	if offset != 0 {
		t.Error("offset != 0\n")
		t.Fail()
	}
	if sequence != 3 {
		t.Error("incorrect sequence")
		t.Fail()
	}

	end = 9
	sequence, offset = getEndOverlapSequenceAndOffset(tail, end)
	if offset != 0 {
		t.Error("offset != 0\n")
		t.Fail()
	}
	if sequence != end {
		t.Error("incorrect sequence")
		t.Fail()
	}

	end = 10
	sequence, offset = getEndOverlapSequenceAndOffset(tail, end)
	if offset != 1 {
		t.Error("offset != 1\n")
		t.Fail()
	}
	if sequence != end-1 {
		t.Error("incorrect sequence")
		t.Fail()
	}

	end = 11
	sequence, offset = getEndOverlapSequenceAndOffset(tail, end)
	if offset != 2 {
		t.Error("offset != 2\n")
		t.Fail()
	}
	if sequence != end-2 {
		t.Error("incorrect sequence")
		t.Fail()
	}
}

func TestGetOverlapBytes(t *testing.T) {
	overlapBytesTests := []struct {
		in   reassemblyInput
		want TestOverlapBytesWant
	}{
		{
			reassemblyInput{3, []byte{2, 3, 4}}, TestOverlapBytesWant{
				bytes:       []byte{6},
				startOffset: 2,
				endOffset:   3,
			},
		},
		{
			reassemblyInput{4, []byte{2, 3, 4}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7},
				startOffset: 1,
				endOffset:   3,
			},
		},
		{
			reassemblyInput{5, []byte{2, 3, 4}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8},
				startOffset: 0,
				endOffset:   3,
			},
		},
		{
			reassemblyInput{6, []byte{1, 2, 3}}, TestOverlapBytesWant{
				bytes:       []byte{7, 8, 9},
				startOffset: 0,
				endOffset:   3,
			},
		},
		{
			reassemblyInput{4, []byte{1, 2, 3, 4, 5, 6, 7}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11},
				startOffset: 1,
				endOffset:   7,
			},
		},
		{
			reassemblyInput{3, []byte{1, 2, 3, 4, 5, 6, 7}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10},
				startOffset: 2,
				endOffset:   7,
			},
		},
		{
			reassemblyInput{34, []byte{1, 2, 3, 4, 5, 6, 7}}, TestOverlapBytesWant{
				bytes:       []byte{35, 36, 37, 38, 39, 40},
				startOffset: 0,
				endOffset:   6,
			},
		},
		{
			reassemblyInput{34, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}, TestOverlapBytesWant{
				bytes:       []byte{35, 36, 37, 38, 39, 40},
				startOffset: 0,
				endOffset:   6,
			},
		},
		{
			reassemblyInput{5, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 0,
				endOffset:   35,
			},
		},

		{
			reassemblyInput{5, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 0,
				endOffset:   35,
			},
		},

		{
			reassemblyInput{5, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 0,
				endOffset:   35,
			},
		},

		{
			reassemblyInput{4, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 1,
				endOffset:   36,
			},
		},

		{
			reassemblyInput{3, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 2,
				endOffset:   37,
			},
		},

		{
			reassemblyInput{4, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}, TestOverlapBytesWant{
				bytes:       []byte{6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40},
				startOffset: 1,
				endOffset:   36,
			},
		},
	}
	options := ConnectionOptions{
		MaxBufferedPagesTotal:         0,
		MaxBufferedPagesPerConnection: 0,
		MaxRingPackets:                40,
		CloseRequestChan:              nil,
		Pager:                         nil,
		LogDir:                        "fake-log-dir",
	}
	conn := NewConnection(&options)

	for j := 5; j < 40; j += 5 {
		conn.ClientStreamRing.Value = types.Reassembly{
			Seq:   types.Sequence(j),
			Bytes: []byte{byte(j + 1), byte(j + 2), byte(j + 3), byte(j + 4), byte(j + 5)},
		}
		conn.ClientStreamRing = conn.ClientStreamRing.Next()
	}
	for i := 0; i < len(overlapBytesTests); i++ {
		var startSeq uint32 = overlapBytesTests[i].in.Seq
		start := types.Sequence(startSeq)
		end := start.Add(len(overlapBytesTests[i].in.Payload) - 1)
		p := PacketManifest{
			IP: layers.IPv4{
				SrcIP:    net.IP{1, 2, 3, 4},
				DstIP:    net.IP{2, 3, 4, 5},
				Version:  4,
				TTL:      64,
				Protocol: layers.IPProtocolTCP,
			},
			TCP: layers.TCP{
				Seq:     startSeq,
				SrcPort: 1,
				DstPort: 2,
			},
			Payload: overlapBytesTests[i].in.Payload,
		}

		ipFlow, _ := gopacket.FlowFromEndpoints(layers.NewIPEndpoint(net.IPv4(1, 2, 3, 4)), layers.NewIPEndpoint(net.IPv4(2, 3, 4, 5)))
		tcpFlow, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(layers.TCPPort(1)), layers.NewTCPPortEndpoint(layers.TCPPort(2)))
		conn.serverFlow = types.NewTcpIpFlowFromFlows(ipFlow, tcpFlow)
		conn.clientFlow = conn.serverFlow.Reverse()

		head, tail := getOverlapRings(p, conn.serverFlow, conn.ClientStreamRing)
		if head == nil || tail == nil {
			t.Errorf("%d getOverlapRings returned a nil\n", i)
			t.Fail()
			continue
		}

		overlapBytes, startOffset, endOffset := getOverlapBytes(head, tail, start, end)

		if startOffset != overlapBytesTests[i].want.startOffset {
			t.Errorf("test %d startOffset %d does not match want.startOffset %d\n", i, startOffset, overlapBytesTests[i].want.startOffset)
			t.Fail()
		}
		if endOffset != overlapBytesTests[i].want.endOffset {
			t.Errorf("test %d endOffset %d does not match want.endOffset %d\n", i, endOffset, overlapBytesTests[i].want.endOffset)
			t.Fail()
		}
		if len(overlapBytes) != len(overlapBytesTests[i].want.bytes) {
			t.Errorf("test %d overlapBytes len %d not equal to want.bytes len %d\n", i, len(overlapBytes), len(overlapBytesTests[i].want.bytes))
			t.Fail()
		}
		if !bytes.Equal(overlapBytes, overlapBytesTests[i].want.bytes) {
			t.Errorf("test %d overlapBytes %x not equal to want.bytes %x\n", i, overlapBytes, overlapBytesTests[i].want.bytes)
			t.Fail()
		}
	}
}

func TestGetOverlapRingsWithZeroRings(t *testing.T) {
	ip := layers.IPv4{
		SrcIP:    net.IP{1, 2, 3, 4},
		DstIP:    net.IP{2, 3, 4, 5},
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
	}
	tcp := layers.TCP{
		SYN:     true,
		SrcPort: 1,
		DstPort: 2,
	}
	tcp.SetNetworkLayerForChecksum(&ip)
	payload := gopacket.Payload([]byte{1, 2, 3, 4})
	p := PacketManifest{
		IP:      ip,
		TCP:     tcp,
		Payload: payload,
	}
	options := ConnectionOptions{
		MaxBufferedPagesTotal:         0,
		MaxBufferedPagesPerConnection: 0,
		MaxRingPackets:                40,
		CloseRequestChan:              nil,
		Pager:                         nil,
		LogDir:                        "fake-log-dir",
	}
	conn := NewConnection(&options)

	ipFlow, _ := gopacket.FlowFromEndpoints(layers.NewIPEndpoint(net.IPv4(1, 2, 3, 4)), layers.NewIPEndpoint(net.IPv4(2, 3, 4, 5)))
	tcpFlow, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(layers.TCPPort(1)), layers.NewTCPPortEndpoint(layers.TCPPort(2)))
	conn.serverFlow = types.NewTcpIpFlowFromFlows(ipFlow, tcpFlow)
	conn.clientFlow = conn.serverFlow.Reverse()

	head, tail := getOverlapRings(p, conn.serverFlow, conn.ClientStreamRing)
	if head == nil || tail == nil {
		return
	} else {
		t.Fail()
	}
	return
}

func TestGetOverlapRings(t *testing.T) {
	overlapTests := []struct {
		in   reassemblyInput
		want []*types.Reassembly
	}{
		{
			reassemblyInput{7, []byte{1, 2}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 5,
				},
			},
		},
		{
			reassemblyInput{5, []byte{1, 2, 3, 4, 5}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 5,
				},
			},
		},
		{
			reassemblyInput{5, []byte{1, 2, 3, 4, 5, 6}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 10,
				},
			},
		},
		{
			reassemblyInput{6, []byte{1, 2, 3, 4, 5}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 10,
				},
			},
		},
		{
			reassemblyInput{7, []byte{1, 2, 3, 4, 5}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 10,
				},
			},
		},
		{
			reassemblyInput{32, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 30,
				},
				&types.Reassembly{
					Seq: 35,
				},
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3}}, []*types.Reassembly{
				nil,
				nil,
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3, 4, 5, 6, 7, 8}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 5,
				},
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3, 4, 5, 6}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 5,
				},
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3}}, []*types.Reassembly{
				nil,
				nil,
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3, 4, 5}}, []*types.Reassembly{
				nil,
				nil,
			},
		},
		{
			reassemblyInput{0, []byte{1, 2, 3, 4, 5, 6}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 5,
				},
				&types.Reassembly{
					Seq: 5,
				},
			},
		},
		{
			reassemblyInput{40, []byte{1}}, []*types.Reassembly{
				nil,
				nil,
			},
		},
		{
			reassemblyInput{42, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}}, []*types.Reassembly{
				nil,
				nil,
			},
		},
		{
			reassemblyInput{38, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}, []*types.Reassembly{
				&types.Reassembly{
					Seq: 35,
				},
				&types.Reassembly{
					Seq: 35,
				},
			},
		},
	}

	ip := layers.IPv4{
		SrcIP:    net.IP{1, 2, 3, 4},
		DstIP:    net.IP{2, 3, 4, 5},
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
	}
	options := ConnectionOptions{
		MaxBufferedPagesTotal:         0,
		MaxBufferedPagesPerConnection: 0,
		MaxRingPackets:                40,
		CloseRequestChan:              nil,
		Pager:                         nil,
		LogDir:                        "fake-log-dir",
	}
	conn := NewConnection(&options)
	for j := 5; j < 40; j += 5 {
		conn.ClientStreamRing.Value = types.Reassembly{
			Seq:   types.Sequence(j),
			Bytes: []byte{1, 2, 3, 4, 5},
		}
		conn.ClientStreamRing = conn.ClientStreamRing.Next()
	}

	for i := 0; i < len(overlapTests); i++ {
		tcp := layers.TCP{
			Seq:     overlapTests[i].in.Seq,
			SYN:     false,
			SrcPort: 1,
			DstPort: 2,
		}
		p := PacketManifest{
			IP:      ip,
			TCP:     tcp,
			Payload: overlapTests[i].in.Payload,
		}

		ipFlow, _ := gopacket.FlowFromEndpoints(layers.NewIPEndpoint(net.IPv4(1, 2, 3, 4)), layers.NewIPEndpoint(net.IPv4(2, 3, 4, 5)))
		tcpFlow, _ := gopacket.FlowFromEndpoints(layers.NewTCPPortEndpoint(layers.TCPPort(1)), layers.NewTCPPortEndpoint(layers.TCPPort(2)))
		conn.serverFlow = types.NewTcpIpFlowFromFlows(ipFlow, tcpFlow)
		conn.clientFlow = conn.serverFlow.Reverse()

		head, tail := getOverlapRings(p, conn.serverFlow, conn.ClientStreamRing)

		if overlapTests[i].want[0] == nil {
			if head != nil {
				t.Error("getOverlapRings did not return a nil ring segment head\n")
				t.Fail()
			}
			if tail != nil {
				t.Error("getOverlapRings did not return a nil ring segment tail\n")
				t.Fail()
			}
			continue
		}
		if head == nil || tail == nil {
			t.Error("head or tail is nil\n")
			t.Fail()
		}
		reassembly, ok := head.Value.(types.Reassembly)
		if overlapTests[i].want[0] != nil {
			if ok {
				if reassembly.Seq.Difference(overlapTests[i].want[0].Seq) != 0 {
					t.Errorf("test %d: reassembly.Seq %d != want.Seq %d\n", i, reassembly.Seq, overlapTests[i].want[0].Seq)
					t.Fail()
				}
			} else {
				t.Error("head.Value is not a types.Reassembly\n")
				t.Fail()
			}
		}
		reassembly, ok = tail.Value.(types.Reassembly)
		if overlapTests[i].want[1] != nil {
			if ok {
				if reassembly.Seq.Difference(overlapTests[i].want[1].Seq) != 0 {
					t.Errorf("test num %d in.Seq %d != want.Seq %d\n", i, reassembly.Seq, overlapTests[i].want[1].Seq)
					t.Fail()
				}
			} else {
				t.Error("tail.Value is not a types.Reassembly\n")
				t.Fail()
			}
		}
	}
	return
}
