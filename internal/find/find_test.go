package find

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain zet de poorten na de console op poort 1, waar niemand luistert:
// anders zou een test op 127.0.0.1 de echte :8080 en :80 van deze machine
// vragen. Tests die Hop nodig hebben zetten hun eigen nepserver.
func TestMain(m *testing.M) {
	agentPort, webPort = 1, 1
	os.Exit(m.Run())
}

// fakeHop zet een nep-agent-API op agentPort zolang de test loopt.
func fakeHop(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	old := agentPort
	agentPort = srv.Listener.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { agentPort = old })
}

// ringOnly is wat een v3-node na een paar uur op 5555 teruggeeft: de 32 KiB
// ring is vol met lopende regels, de boot is eruit (letterlijk van een O6N op
// rc8, 08-11).
const ringOnly = `dvfs: clock 2600 MHz (fastest domain) (full), temp 42.0 C, busy slot 2 (953 permille idle over 9 cores) HOPOS_CLOCK
disk: commands=122795 ahead_hits=2 ahead_waste=2 slowest_us=4075 free_pages=512 queue_peak=1 queue_polls=371806 HOPOS_DISK_STATS
slot 3: SPIN_SERVICE_CONNECT_FAILED reason=upgrade timed out retry_in=30s
`

// TestHopWeetWatDeRingVergat: de boot is uit de ring, Hop weet het nog.
func TestHopWeetWatDeRingVergat(t *testing.T) {
	fakeHop(t, 200, `[{"id":"other-1","endpoint":"http://10.9.9.9:8080","version":"3.1.0"},`+
		`{"id":"o6n-1","endpoint":"http://127.0.0.1:8080","version":"3.2.0-rc8","temp_milli_c":42000}]`)
	n, ok := probe(context.Background(), "127.0.0.1", serve(t, ringOnly))
	if !ok {
		t.Fatal("node met een volle ring niet herkend")
	}
	if n.Name != "o6n-1" || n.Version != "3.2.0-rc8" || n.State != "agent up" || !n.InsecND {
		t.Errorf("node = %+v", n)
	}
	if n.Go || n.Flavor != "" {
		t.Errorf("zonder boot in de ring: Go=%v flavor=%q, wil false en leeg", n.Go, n.Flavor)
	}
}

// TestHopMetSleutel: een 401 zegt dat Hop draait, en niets meer.
func TestHopMetSleutel(t *testing.T) {
	fakeHop(t, 401, `{"error":"unauthorized"}`)
	n, ok := probe(context.Background(), "127.0.0.1", serve(t, ringOnly))
	if !ok {
		t.Fatal("niet herkend")
	}
	if n.State != "agent up" || n.InsecND || n.Name != "?" {
		t.Errorf("node = %+v", n)
	}
}

// bootLog is een HopOS v3-boot (Rust) zoals hij over tcp/5555 komt: de
// bunny, de runtime-regel, het glas, het netwerk, de env van Hop, Hop zelf en
// welcome. Ingekort maar met álle regels die find.parse gebruikt, letterlijk
// uit de bronnen (HopOS: hopos/src/main.rs, gui.rs, net.rs, config.rs,
// slots.rs, apps/welcome; hop: agentd-hopos/src/main.rs). Drie %s: arch, ip
// en naam — de naam staat er twee keer, één keer per bron.
const bootLog = `
   (\(\
   ( -.-)     HopOS
   o_(")(")   --------------
              the Rust-only OS

runtime rustc 1.91.0 (f8297e351 2025-10-28) none/%[1]s (HopOS v3.2.0)
fb: console on 1920x1080 stride 7680 @ 0x3e8fa000 (240x67 cells) HOPOS_FB_CONSOLE
net: %[2]s (mac 5a:be:49:3e:64:98, gw 192.168.1.1) HOPOS_NET_UP
console: also on tcp/5555 (%[2]s and 10.0.0.1): nc, first the history, then live HOPOS_CONPORT_UP
slots: Hop env: HOPOS_NODE=%[3]s HOPOS_CLUSTER=hopos HOPOS_INSECURE=1 HOPOS_INIT_JOBS=<1 jobs, 412 bytes> HOPOS_HOP_ENV
slots: Hop runs with HOPOS_INSECURE=1: API authentication is OFF HOPOS_HOP_INSECURE
HOPOS_HOP_START slot=1 core=0 cpu=0 entry=0x200000 part=0x80000000+0x4000000 image=1835008 env=611 class=big
slot 1: hop: agent up node=%[3]s cluster=hopos agent=:8080 leader=:9080 cores=3 system=10.0.0.1:10100 HOP_UP
slot 2: welcome: serving http on 10.0.0.2:80 for node %[3]s, slot 2, 4 workers HOPOS_WELCOME_UP port=80
`

// bootLogGo is een boot van de Go-generatie (HopOS v2), letterlijk uit
// metal/cmd/hopos/main.go, net/hopnet en board/rk3566/hop op tag v2.2.8.
// FIND blijft die herkennen: er draaien nog Go-nodes.
const bootLogGo = `
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

// headlessOf haalt de regels met beeld uit een boot: een headless kern (v3
// zonder gui-feature, v2 zonder display) meldt ze niet.
func headlessOf(log string) string {
	var keep []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "fb: console on") || strings.HasPrefix(l, "display:") ||
			strings.HasPrefix(l, "console: framebuffer") || strings.Contains(l, "HOPOS_WELCOME_UP") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}

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
		name, kernArch, arch, ip, flavor string
	}{
		{"radxa-1", "aarch64", "arm64", "192.168.1.51", "headfull"},
		{"rpi4-4c54", "aarch64", "arm64", "192.168.1.52", "headfull"},
		{"licheerv-0a1b", "riscv64", "riscv64", "192.168.1.53", "headless"},
	}
	for _, w := range want {
		log := fmt.Sprintf(bootLog, w.kernArch, w.ip, w.name)
		if w.flavor == "headless" {
			log = headlessOf(log)
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
		if n.Version != "3.2.0" {
			t.Errorf("%s: versie = %q, wil 3.2.0", w.name, n.Version)
		}
		if n.Flavor != w.flavor {
			t.Errorf("%s: flavor = %q, wil %q", w.name, n.Flavor, w.flavor)
		}
		if n.Go {
			t.Errorf("%s: v3-node als Go gezien", w.name)
		}
		if n.Web != (w.flavor == "headfull") {
			t.Errorf("%s: web = %v, wil %v", w.name, n.Web, w.flavor == "headfull")
		}
		if n.State != "agent up" {
			t.Errorf("%s: state = %q, wil %q", w.name, n.State, "agent up")
		}
		if !n.InsecND {
			t.Errorf("%s: open API niet gezien", w.name)
		}
	}
}

// TestNaamUitDeEnvVoorHop: zolang Hop nog niet op is, staat de naam al in de
// env die de kern hem meegaf — en de staat zegt dat Hop start.
func TestNaamUitDeEnvVoorHop(t *testing.T) {
	var keep []string
	for _, l := range strings.Split(fmt.Sprintf(bootLog, "aarch64", "192.168.1.60", "o6n-0007"), "\n") {
		if strings.Contains(l, "HOP_UP") || strings.Contains(l, "WELCOME_UP") {
			continue
		}
		keep = append(keep, l)
	}
	n := parse("192.168.1.60", strings.Join(keep, "\n"))
	if n.Name != "o6n-0007" {
		t.Errorf("naam = %q, wil o6n-0007", n.Name)
	}
	if n.State != "hop starting" {
		t.Errorf("state = %q, wil hop starting", n.State)
	}
}

// TestGoNodeBlijftHerkend: een node van de Go-generatie (v2) staat er nog
// steeds in, met zijn eigen woord voor beeld (desktop) en zonder versie.
func TestGoNodeBlijftHerkend(t *testing.T) {
	for _, w := range []struct {
		name, arch, flavor string
	}{
		{"pi5-kok", "arm64", "desktop"},
		{"lrv-nano", "riscv64", "headless"},
	} {
		log := fmt.Sprintf(bootLogGo, w.arch, "192.168.1.70", w.name)
		if w.flavor == "headless" {
			log = headlessOf(log)
		}
		port := serve(t, log)
		n, ok := probe(context.Background(), "127.0.0.1", port)
		if !ok {
			t.Fatalf("Go-node %s niet herkend", w.name)
		}
		if n.Name != w.name || n.Arch != w.arch || n.Flavor != w.flavor || n.Version != "" || !n.Go {
			t.Errorf("Go-node = %+v, wil naam %s arch %s smaak %s zonder versie", n, w.name, w.arch, w.flavor)
		}
		if n.State != "agent up" || !n.InsecND {
			t.Errorf("%s: state %q insecure %v", w.name, n.State, n.InsecND)
		}
	}
}

// TestScanMeldtElkeVondst: Scan moet élke node los melden (de UI vult zijn
// lijst daarmee terwijl de scan nog loopt) en de voortgang blijven tellen.
func TestScanMeldtElkeVondst(t *testing.T) {
	port := serve(t, fmt.Sprintf(bootLog, "aarch64", "192.168.1.51", "radxa-1"))
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
