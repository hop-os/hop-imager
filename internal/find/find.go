// Package find zoekt draaiende HopOS-nodes op het lokale netwerk — het
// antwoord op "waar staat dat ding nou?" zonder router-webinterface.
//
// HOE: een node serveert zijn console op tcp/5555 (HopOS v3: kern/conport en
// hopos/src/net.rs; aan met hopos.console=1 of hopos.insecure=1, en beide
// meegeleverde configs, headless.cfg en headfull.cfg, zetten hem aan). Wie
// daar verbindt krijgt meteen de BEWAARDE console-geschiedenis, en die begint
// bij de boot — dus met de bunny en de HOPOS_*-markers. Eén TCP-connect identificeert de node dus
// niet alleen, hij levert ook zijn naam, IP, architectuur en of er beeld aan
// hangt. Geen mDNS, geen broadcast, geen extra code op de node: het kanaal dat
// er al is, gelezen zoals het bedoeld is.
//
// MAAR: de console is een ring van 32 KiB. Een v3-node die een paar uur draait
// heeft zijn boot er al uit geschoven, en na een kernel-flip start Hop niet
// opnieuw (geen HOP_UP). Dus vraagt FIND een node die zich op 5555 als HopOS
// liet kennen daarna aan Hop zelf: GET :8080/v1/agents geeft zijn naam (id) en
// versie, ook dagen na de boot. 8080 is in v3 de agent-poort van Hop, van ons
// dus, en we vragen het alleen aan wat al HopOS bleek. En of er iets op poort 80
// luistert (welcome, of wat de node daar ook publiceert): dat is de Web-knop.
//
// Wat we NIET doen: andere poorten scannen, of buiten het eigen subnet kijken.
// De scan blijft binnen wat het OS als lokaal netwerk kent.
package find

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// debugProbe zet de meting aan (tijdelijk, tijdens het jagen op een node die
// niet gevonden werd).
var debugProbe = os.Getenv("HOPFIND_DEBUG") != ""

// DefaultPort is de console-poort van de kern (CONSOLE_PORT in
// hopos/src/net.rs, conport in de Go-generatie).
const DefaultPort = 5555

// De poorten die FIND na de console nog aan een HopOS-node vraagt. Variabelen,
// zodat de tests ze op een nepnode kunnen zetten.
var (
	agentPort = 8080 // Hop's agent-API (hop: agentd-hopos)
	webPort   = 80   // wat de node publiceert; headfull.cfg zet welcome hier
)

// Node is één gevonden HopOS-node.
type Node struct {
	IP      string
	Name    string // hopos.node, uit de HOP_UP-regel van Hop (v2: HOPOS_AGENT_UP)
	Arch    string // "arm64" / "riscv64", uit de runtime-regel
	Version string // "3.2.0", van Hop (/v1/agents) of uit de runtime-regel
	Go      bool   // een node van de Go-generatie (v2): tamago, "the Go-only OS"
	Flavor  string // "headfull" (v3) of "desktop" (v2) met beeld, "headless" zonder, "" als de boot niet meer in de ring staat
	State   string // korte samenvatting: agent up / headless / booting
	InsecND bool   // API zonder auth (HOPOS_API_INSECURE, HOPOS_HOP_INSECURE)
	Web     bool   // er luistert iets op poort 80 (welcome, HOPOS_WELCOME_UP)
	Console string // de bewaarde console zoals we hem lazen
}

var (
	// De nodenaam mag ALLEEN uit een marker-regel komen. Op de hele log
	// zoeken kostte een valse naam: de waarschuwing "any host that can reach
	// this node can dispatch jobs" gaf keurig "can" (gevonden door de test in
	// find_test.go, 06-08). Elke regex hier hoort dus bij één HOPOS_*-marker.
	// Case-insensitive op "node", maar de NAAM houdt zijn eigen schrijfwijze:
	// de melding zonder API-sleutel begint met "Node <naam> stays alive".
	//
	// HopOS v3 (Rust) zet de naam als sleutel=waarde in de regel: Hop meldt
	// "hop: agent up node=<naam> cluster=… HOP_UP", de kern daarvoor al
	// "slots: Hop env: HOPOS_NODE=<naam> … HOPOS_HOP_ENV". De Go-generatie
	// (v2) schreef "node <naam>,".
	reNode = regexp.MustCompile(`(?i)\bnode(?: |=)([^\s,]+)`)
	// v3: "runtime rustc 1.91.0 (f8297e351 2025-10-28) none/aarch64 (HopOS v3.2.0)"
	// v2: "runtime go1.26.4 tamago/arm64"
	reArch    = regexp.MustCompile(`(?:none|tamago)/(\w+)`)
	reVersion = regexp.MustCompile(`\(HopOS v([^)\s]+)\)`)
	reHopUp   = regexp.MustCompile(`\bHOP_UP\b`)
	// De regels die de naam écht dragen, in volgorde van betrouwbaarheid: eerst
	// v3 (Hop zelf, dan de env die de kern hem gaf), dan v2.
	nameMarkers = []*regexp.Regexp{
		reHopUp,
		regexp.MustCompile(`\bHOPOS_HOP_ENV\b`),
		regexp.MustCompile(`\bHOPOS_AGENT_UP\b`),
		regexp.MustCompile(`\bHOPOS_NODE_HEADLESS\b`),
		regexp.MustCompile(`\bHOPOS_API_NO_AUTH\b`),
	}
	reEnvNode = regexp.MustCompile(`\bHOPOS_NODE=([^\s,]+)`)
)

// nameFrom haalt de nodenaam uit de regel die een naam-marker draagt.
func nameFrom(text string) string {
	for _, marker := range nameMarkers {
		for _, line := range strings.Split(text, "\n") {
			if !marker.MatchString(line) {
				continue
			}
			if m := reEnvNode.FindStringSubmatch(line); m != nil {
				return m[1]
			}
			if m := reNode.FindStringSubmatch(line); m != nil {
				// De naam zelf mag geen woord uit de melding zijn: hij staat
				// altijd direct vóór een komma of "stays".
				return strings.Trim(m[1], ".,;")
			}
		}
	}
	return "?"
}

// Subnets geeft de IPv4-subnetten van dit apparaat, elk als lijst hosts. Grote
// netten worden op 1024 adressen afgekapt: een /16 volledig scannen duurt
// minuten en levert niets extra op een thuis- of banknetwerk.
func Subnets() []string {
	var hosts []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			ones, bits := n.Mask.Size()
			if bits != 32 || ones < 22 || ones > 30 {
				continue // te groot om te scannen of een puntverbinding
			}
			base := n.IP.Mask(n.Mask).To4()
			count := 1 << (32 - ones)
			if count > 1024 {
				count = 1024
			}
			for i := 1; i < count-1; i++ {
				ip := net.IPv4(base[0], base[1], base[2], base[3]).To4()
				v := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
				v += uint32(i)
				s := fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
				if !seen[s] {
					seen[s] = true
					hosts = append(hosts, s)
				}
			}
		}
	}
	return hosts
}

// Stats vat een scan samen: hoeveel adressen geprobeerd, hoeveel nodes gevonden.
type Stats struct {
	Tried int
	Found int
}

// Scan probeert elk adres en meldt elke gevonden node via found (van een
// willekeurige goroutine — de aanroeper zorgt voor de UI-thread). progress
// wordt na elke poging aangeroepen.
func Scan(ctx context.Context, hosts []string, port int, found func(Node), progress func(done, total int)) Stats {
	const workers = 128
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		done  int
		stats = Stats{Tried: len(hosts)}
		ch    = make(chan string)
	)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ch {
				n, ok := probe(ctx, ip, port)
				mu.Lock()
				done++
				d := done
				if ok {
					stats.Found++
				}
				mu.Unlock()
				if ok && found != nil {
					found(n)
				}
				if progress != nil {
					progress(d, len(hosts))
				}
			}
		}()
	}
	for _, ip := range hosts {
		select {
		case <-ctx.Done():
			close(ch)
			wg.Wait()
			return stats
		case ch <- ip:
		}
	}
	close(ch)
	wg.Wait()
	return stats
}

// probe verbindt, leest kort mee en beslist of dit een HopOS-node is.
//
// De leesdeadline is de hele truc: de node stuurt zijn geschiedenis in één
// keer zodra we verbinden (conport.stream), dus 700ms is genoeg voor een volle
// boot-log — en kort genoeg om 254 adressen in seconden te doen.
func probe(ctx context.Context, ip string, port int) (Node, bool) {
	d := net.Dialer{Timeout: 400 * time.Millisecond}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprint(port)))
	if err != nil {
		if debugProbe {
			fmt.Println("DEBUG dial", ip, err)
		}
		return Node{}, false
	}
	defer c.Close()
	text := read(c, 700*time.Millisecond, 256<<10)
	if debugProbe {
		fmt.Printf("DEBUG %s: %d bytes, hopos=%v, eerste=%q\n", ip, len(text), isHopOS(text), text[:min(len(text), 80)])
	}
	if !isHopOS(text) {
		return Node{}, false
	}
	n := parse(ip, text)
	if !n.Go {
		askHop(ctx, &n)
	}
	if !n.Web {
		n.Web = listens(ctx, ip, webPort)
	}
	return n, true
}

// agent is wat Hop in /v1/agents over een node zegt; de rest negeren we.
type agent struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
	Version  string `json:"version"`
}

var hopClient = &http.Client{Timeout: 1500 * time.Millisecond}

// askHop vult aan wat de console niet (meer) weet. Een 200 zonder sleutel: Hop
// draait en zijn API is open — naam en versie uit zijn eigen agent-regel. Een
// 401/403: Hop draait, met een sleutel; meer zegt hij ons niet, en dat hoort zo.
// Geen antwoord: de console blijft de bron.
func askHop(ctx context.Context, n *Node) {
	url := fmt.Sprintf("http://%s/v1/agents", net.JoinHostPort(n.IP, fmt.Sprint(agentPort)))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	resp, err := hopClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		if !strings.HasPrefix(n.State, "parked") {
			n.State = "agent up"
		}
		n.InsecND = false
		return
	default:
		return
	}
	var agents []agent
	if json.NewDecoder(resp.Body).Decode(&agents) != nil {
		return
	}
	n.State, n.InsecND = "agent up", true
	// In een cluster staan alle agents erin: de onze is die met ons adres.
	var mine *agent
	for i, a := range agents {
		if strings.Contains(a.Endpoint, "//"+n.IP+":") {
			mine = &agents[i]
		}
	}
	if mine == nil && len(agents) == 1 {
		mine = &agents[0]
	}
	if mine == nil {
		return
	}
	if mine.ID != "" {
		n.Name = mine.ID
	}
	if mine.Version != "" {
		n.Version = mine.Version
	}
}

// listens: neemt iemand op deze poort op? Alleen een connect, niets erover.
func listens(ctx context.Context, ip string, port int) bool {
	d := net.Dialer{Timeout: 400 * time.Millisecond}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprint(port)))
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// Console leest de console van één node — hetzelfde kanaal, alleen langer, om
// hem in de tool te kunnen tonen (`nc <node> 5555` in een venster).
func Console(ip string, port int, d time.Duration) (string, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(port)), 2*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	return read(c, d, 1<<20), nil
}

func read(c net.Conn, within time.Duration, max int) string {
	c.SetReadDeadline(time.Now().Add(within))
	var b strings.Builder
	buf := make([]byte, 32<<10)
	for b.Len() < max {
		n, err := c.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return b.String()
}

// isHopOS beslist op de markers die ALLEEN een HopOS-console heeft: de
// HOPOS_*-tags of de bunny-kop. Niet op "HopOS" alleen — dat zou elke
// willekeurige logstream met dat woord erin oppikken. De tagline is "the
// Rust-only OS" sinds v3 en was "the Go-only OS" in v2; allebei goed.
func isHopOS(text string) bool {
	return strings.Contains(text, "HOPOS_") ||
		strings.Contains(text, `o_(")(")`) ||
		(strings.Contains(text, "HopOS") && strings.Contains(text, "-only OS"))
}

func parse(ip, text string) Node {
	n := Node{IP: ip, Console: text, Arch: "?", State: "booting"}
	n.Name = nameFrom(text)
	n.Go = strings.Contains(text, "tamago/") || strings.Contains(text, "the Go-only OS")
	if m := reArch.FindStringSubmatch(text); m != nil {
		// De runtime-regel staat er: de boot zit nog in de ring, dus wat er
		// níet staat (een framebuffer) is er ook niet.
		n.Flavor = "headless"
		n.Arch = m[1]
		// De kern van v3 zegt aarch64 (de Rust-target); jobs en de rest van
		// Hop zeggen arm64 ("node.arch":"arm64"). De tool toont dat laatste.
		if n.Arch == "aarch64" {
			n.Arch = "arm64"
		}
	}
	if m := reVersion.FindStringSubmatch(text); m != nil {
		n.Version = m[1]
	}
	// Beeld. v3: de kern krijgt een framebuffer voor zijn console
	// (HOPOS_FB_CONSOLE, alleen in een kern met de gui-feature) of meldt een
	// scanout op HDMI. v2: de log gespiegeld op een framebuffer. Alle drie
	// betekenen: hier hangt glas aan.
	switch {
	case strings.Contains(text, "HOPOS_FB_CONSOLE") ||
		(n.Version != "" && strings.Contains(text, "on HDMI")):
		n.Flavor = "headfull"
	case strings.Contains(text, "on HDMI") || strings.Contains(text, "mirroring log to display"):
		n.Flavor = "desktop"
	}
	switch {
	case reHopUp.MatchString(text), strings.Contains(text, "HOPOS_AGENT_UP"):
		n.State = "agent up"
	case strings.Contains(text, "HOPOS_HOP_DOWN"):
		n.State = "hop down"
	case strings.Contains(text, "HOPOS_NODE_HEADLESS"):
		n.State = "no network — parked"
	case strings.Contains(text, "HOPOS_HOP_NO_AUTH"), strings.Contains(text, "HOPOS_API_NO_AUTH"):
		n.State = "parked — no api key set"
	case strings.Contains(text, "HOPOS_HOP_START"):
		n.State = "hop starting"
	case strings.Contains(text, "HOPOS_NET_UP"):
		n.State = "network up"
	}
	n.InsecND = strings.Contains(text, "HOPOS_API_INSECURE") || strings.Contains(text, "HOPOS_HOP_INSECURE")
	n.Web = strings.Contains(text, "HOPOS_WELCOME_UP")
	return n
}
