package main

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type TraceInfo struct {
	filename string
	packets  int
	bytes    []int
}

var traces = [...]TraceInfo{
	{
		filename: "eth-ip4-arp-dns-req-http-google.pcap",
		packets:  58,
		bytes:    []int{44, 44, 76, 76, 104, 156, 76, 76, 68, 180, 68, 556, 68, 76, 76, 92, 104, 76, 76, 68, 216, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 68, 1416, 68, 1416, 68, 1416, 68, 1416, 68, 1080, 68, 68, 68, 68, 68, 68},
	},
}

func replayTraceHelper(t *testing.T, trace string, target string) {
	t.Log("Replaying", trace)
	out, err := exec.Command("go", "run", "../cmd/pcap2sflow-replay/pcap2sflow-replay.go", "-trace", trace, target).CombinedOutput()
	if err != nil {
		t.Error(err.Error() + "\n" + string(out))
	}
	t.Log("Stdout/Stderr ", string(out))
}

func sflowSetup(t *testing.T) (*net.UDPConn, error) {
	addr := net.UDPAddr{
		Port: 0,
		IP:   net.ParseIP("localhost"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		t.Errorf("Unable to listen on UDP %s", err.Error())
		return nil, err
	}
	return conn, nil
}

const (
	maxDgramSize = 16384
)

func asyncSflowListen(t *testing.T, wg *sync.WaitGroup, conn *net.UDPConn, trace *TraceInfo) {
	defer wg.Done()

	var buf [maxDgramSize]byte
	t.Log("listen...")
	nbPackets := 0

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := conn.ReadFromUDP(buf[:])
		if err != nil {
			neterr := err.(*net.OpError)
			if neterr.Timeout() == false {
				t.Error(err.Error())
			}
			break
		}

		p := gopacket.NewPacket(buf[:], layers.LayerTypeSFlow, gopacket.Default)
		sflowLayer := p.Layer(layers.LayerTypeSFlow)
		sflowPacket, ok := sflowLayer.(*layers.SFlowDatagram)
		if !ok {
			t.Fatal("not SFlowDatagram")
			break
		}

		if sflowPacket.SampleCount > 0 {
			for _, sample := range sflowPacket.FlowSamples {
				for _, rec := range sample.Records {
					record, ok := rec.(layers.SFlowRawPacketFlowRecord)
					if !ok {
						t.Fatal("1st layer is not SFlowRawPacketFlowRecord type")
						break
					}

					packet := record.Header
					nbPackets++
					packetSize := len(packet.Data())

					if nbPackets > len(trace.bytes) {
						t.Fatalf("Too much Packets, reference have only %d", len(trace.bytes))
					}
					if trace.bytes[nbPackets-1] != packetSize {
						t.Fatalf("Packet size don't match %d %d", trace.bytes[nbPackets-1], packetSize)
					}
				}
			}
		}
	}

	if trace.packets != nbPackets {
		t.Fatalf("NB Packets don't match %d %d", trace.packets, nbPackets)
	}
}

func TestPcap2SflowReplay(t *testing.T) {
	conn, err := sflowSetup(t)
	if err != nil {
		t.Fatal("SFlow setup failed", err.Error())
	}
	defer conn.Close()
	laddr, err := net.ResolveUDPAddr(conn.LocalAddr().Network(), conn.LocalAddr().String())
	if err != nil {
		t.Error("Can't read back the local address")
	}

	for _, trace := range traces {
		var wg sync.WaitGroup
		wg.Add(1)

		go asyncSflowListen(t, &wg, conn, &trace)

		fulltrace, _ := filepath.Abs("pcaptraces" + string(filepath.Separator) + trace.filename)
		replayTraceHelper(t, fulltrace, fmt.Sprintf("localhost:%d", laddr.Port))

		wg.Wait()
	}
}
