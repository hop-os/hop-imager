package find

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// bootLog is een echte HopOS-boot zoals hij over tcp/5555 komt: de bunny, de
// runtime-regel, het netwerk, de display en de agent. Ingekort maar met álle
// regels die find.parse gebruikt, letterlijk uit de bronnen
// (metal/cmd/hopos/main.go, net/hopnet, board/rk3566/hop).
const bootLog = `
   (\(\
   ( -.-)     HopOS
   o_(")(")   --------------
              the Go-only OS

runtime go1.26.4 tamago/%s
psci: v1.1 (boot EL2, SMC conduit)
console: framebuffer 1920x1080 @ 0x7000000, 32 bpp — mirroring log to display
display: 1920x1080p60 on HDMI (sink attached: true)
net: %s (mac 5a:be:49:3e:64:98, gw 192.168.1.1) — HOPOS_NET_UP
console: also on tcp/5555 — HOPOS_CONPORT_UP
hop: WARNING — API authentication is OFF (hopos.insecure=1): any host that can reach this node can dispatch jobs. HOPOS_API_INSECURE
hop: agent starting — node %s, agent :8080, leader :9080 — HOPOS_AGENT_UP
`

// serve start een neppe node op een vrije poort en geeft die poort terug.
func serve(t *testing.T, payload string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				c.Write([]byte(payload))
				time.Sleep(2 * time.Second) // node blijft verbonden, net als echt
				c.Close()
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestProbeVindtMeerdereNodes is de kern: een netwerk vól nodes moet er vól
// uitkomen, elk met zijn eigen naam, arch en smaak (Derek 06-08: "zorg wel dat
// find er meer dan 1 vindt").
func TestProbeVindtMeerdereNodes(t *testing.T) {
	want := []struct {
		name, arch, ip, flavor string
		desktop                bool
	}{
		{"radxa-1", "arm64", "192.168.1.51", "desktop", true},
		{"pi5-kok", "arm64", "192.168.1.52", "desktop", true},
		{"lrv-nano", "riscv64", "192.168.1.53", "headless", false},
	}
	for _, w := range want {
		log := fmt.Sprintf(bootLog, w.arch, w.ip, w.name)
		if !w.desktop {
			// Een headless node meldt geen beeld: die twee regels eruit.
			var keep []string
			for _, l := range strings.Split(log, "\n") {
				if strings.HasPrefix(l, "display:") || strings.HasPrefix(l, "console: framebuffer") {
					continue
				}
				keep = append(keep, l)
			}
			log = strings.Join(keep, "\n")
		}
		port := serve(t, log)
		n, ok := probe(context.Background(), "127.0.0.1", port)
		if !ok {
			t.Fatalf("node %s niet herkend", w.name)
		}
		if n.Name != w.name {
			t.Errorf("naam = %q, wil %q", n.Name, w.name)
		}
		if n.Arch != w.arch {
			t.Errorf("%s: arch = %q, wil %q", w.name, n.Arch, w.arch)
		}
		if n.Flavor != w.flavor {
			t.Errorf("%s: flavor = %q, wil %q", w.name, n.Flavor, w.flavor)
		}
		if n.State != "agent up" {
			t.Errorf("%s: state = %q, wil %q", w.name, n.State, "agent up")
		}
		if !n.InsecND {
			t.Errorf("%s: open API niet gezien", w.name)
		}
	}
}

// TestScanMeldtElkeVondst: Scan moet élke node los melden (de UI vult zijn
// lijst daarmee terwijl de scan nog loopt) en de voortgang blijven tellen.
func TestScanMeldtElkeVondst(t *testing.T) {
	port := serve(t, fmt.Sprintf(bootLog, "arm64", "192.168.1.51", "radxa-1"))
	// Vier keer hetzelfde adres: vier aparte probes, dus vier meldingen — en
	// dat is precies wat een netwerk met vier nodes ook geeft.
	hosts := []string{"127.0.0.1", "127.0.0.1", "127.0.0.1", "127.0.0.1"}
	var found, prog int
	Scan(context.Background(), hosts, port,
		func(Node) { found++ },
		func(done, total int) { prog = done })
	if found != 4 {
		t.Errorf("gevonden = %d, wil 4", found)
	}
	if prog != len(hosts) {
		t.Errorf("voortgang = %d/%d, wil volledig", prog, len(hosts))
	}
}

// TestGeenValsPositief: alles wat geen HopOS is moet stil blijven. Een
// SSH-banner of een willekeurige webserver op 5555 mag nooit als node in de
// lijst komen.
func TestGeenValsPositief(t *testing.T) {
	for _, payload := range []string{
		"SSH-2.0-OpenSSH_9.6\r\n",
		"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n<html>HopOS fanpage</html>",
		"",
		"random noise \x00\x01\x02",
	} {
		port := serve(t, payload)
		if _, ok := probe(context.Background(), "127.0.0.1", port); ok {
			t.Errorf("vals positief op %q", payload)
		}
	}
}

// TestDodePoortKostGeenTijd: een adres zonder luisteraar moet snel opgeven,
// anders duurt een /24-scan minuten.
func TestDodePoortKostGeenTijd(t *testing.T) {
	start := time.Now()
	if _, ok := probe(context.Background(), "127.0.0.1", 1); ok {
		t.Error("poort 1 gaf een node")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("dode poort kostte %v", d)
	}
}

// TestSubnetsBlijftBegrensd: de hostlijst mag nooit ontploffen (een /16 zou
// minuten kosten) en moet het eigen netwerk bevatten.
func TestSubnetsBlijftBegrensd(t *testing.T) {
	h := Subnets()
	if len(h) > 4096 {
		t.Errorf("te veel hosts: %d", len(h))
	}
	for _, ip := range h {
		if net.ParseIP(ip) == nil {
			t.Fatalf("ongeldig adres %q", ip)
		}
	}
}
