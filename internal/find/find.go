// Package find zoekt draaiende HopOS-nodes op het lokale netwerk — het
// antwoord op "waar staat dat ding nou?" zonder router-webinterface.
//
// HOE: elke node serveert zijn console op tcp/5555 (kern/conport; álle
// meegeleverde configs zetten hopos.console=5555). Wie daar verbindt krijgt
// meteen de BEWAARDE console-geschiedenis, en die begint bij de boot — dus met
// de bunny en de HOPOS_*-markers. Eén TCP-connect identificeert de node dus
// niet alleen, hij levert ook zijn naam, IP, architectuur en of er beeld aan
// hangt. Geen mDNS, geen broadcast, geen extra code op de node: het kanaal dat
// er al is, gelezen zoals het bedoeld is.
//
// Wat we NIET doen: poorten dichtbij scannen die niet van ons zijn (8080 en 80
// zijn van de apps), of buiten het eigen subnet kijken. De scan blijft binnen
// wat het OS als lokaal netwerk kent.
package find

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// debugProbe zet de meting aan (tijdelijk, tijdens het jagen op een node die
// niet gevonden werd).
var debugProbe = os.Getenv("HOPFIND_DEBUG") != ""

// DefaultPort is wat elke meegeleverde config zet (hopos.console=5555).
const DefaultPort = 5555

// Node is één gevonden HopOS-node.
type Node struct {
	IP      string
	Name    string // hopos.node, uit de HOPOS_AGENT_UP-regel
	Arch    string // "arm64" / "riscv64", uit de runtime-regel
	Flavor  string // "desktop" (er hangt beeld aan) of "headless"
	State   string // korte samenvatting: agent up / headless / booting
	InsecND bool   // API zonder auth (HOPOS_API_INSECURE)
	Console string // de bewaarde console zoals we hem lazen
}

var (
	// De nodenaam mag ALLEEN uit een marker-regel komen. Op de hele log
	// zoeken kostte een valse naam: de waarschuwing "any host that can reach
	// this node can dispatch jobs" gaf keurig "can" (gevonden door de test in
	// find_test.go, 06-08). Elke regex hier hoort dus bij één HOPOS_*-marker.
	// Case-insensitive op "node", maar de NAAM houdt zijn eigen schrijfwijze:
	// de melding zonder API-sleutel begint met "Node <naam> stays alive".
	reNode = regexp.MustCompile(`(?i)\bnode ([^\s,]+)`)
	// "runtime go1.26.4 tamago/arm64"
	reArch = regexp.MustCompile(`tamago/(\w+)`)
	// De regels die de naam écht dragen, in volgorde van betrouwbaarheid.
	nameMarkers = []string{"HOPOS_AGENT_UP", "HOPOS_NODE_HEADLESS", "HOPOS_API_NO_AUTH"}
)

// nameFrom haalt de nodenaam uit de regel die een naam-marker draagt.
func nameFrom(text string) string {
	for _, marker := range nameMarkers {
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, marker) {
				continue
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

// Stats vat een scan samen. Unreachable is het aantal adressen dat METEEN
// "no route to host" gaf: dat is niet "host is er niet" (dan krijg je een
// time-out of een refused), maar macOS dat de app de toegang tot het lokale
// netwerk ontzegt. Zonder dat onderscheid ziet een geblokkeerde app eruit als
// een leeg netwerk — en dat is de ergste soort stilte.
type Stats struct {
	Tried       int
	Found       int
	Unreachable int
}

// Blocked: bijna alles onbereikbaar in nul tijd en niets gevonden = de
// Local-Network-permissie ontbreekt (zie make-app.sh voor het waarom).
func (s Stats) Blocked() bool {
	return s.Found == 0 && s.Tried > 4 && s.Unreachable*4 > s.Tried*3
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
				n, ok, unreach := probe(ctx, ip, port)
				mu.Lock()
				done++
				d := done
				if unreach {
					stats.Unreachable++
				}
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
func probe(ctx context.Context, ip string, port int) (n Node, ok, unreachable bool) {
	d := net.Dialer{Timeout: 400 * time.Millisecond}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, fmt.Sprint(port)))
	if err != nil {
		if debugProbe {
			fmt.Println("DEBUG dial", ip, err)
		}
		return Node{}, false, errors.Is(err, syscall.EHOSTUNREACH)
	}
	defer c.Close()
	text := read(c, 700*time.Millisecond, 256<<10)
	if debugProbe {
		fmt.Printf("DEBUG %s: %d bytes, hopos=%v, eerste=%q\n", ip, len(text), isHopOS(text), text[:min(len(text), 80)])
	}
	if !isHopOS(text) {
		return Node{}, false, false
	}
	return parse(ip, text), true, false
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
// willekeurige logstream met dat woord erin oppikken.
func isHopOS(text string) bool {
	return strings.Contains(text, "HOPOS_") ||
		strings.Contains(text, `o_(")(")`) ||
		(strings.Contains(text, "HopOS") && strings.Contains(text, "the Go-only OS"))
}

func parse(ip, text string) Node {
	n := Node{IP: ip, Console: text, Arch: "?", Flavor: "headless", State: "booting"}
	n.Name = nameFrom(text)
	if m := reArch.FindStringSubmatch(text); m != nil {
		n.Arch = m[1]
	}
	// Beeld: de node meldt óf een scanout op HDMI, óf dat hij de log op een
	// framebuffer spiegelt. Beide betekenen: hier hangt glas aan.
	if strings.Contains(text, "on HDMI") || strings.Contains(text, "mirroring log to display") {
		n.Flavor = "desktop"
	}
	switch {
	case strings.Contains(text, "HOPOS_AGENT_UP"):
		n.State = "agent up"
	case strings.Contains(text, "HOPOS_NODE_HEADLESS"):
		n.State = "no network — parked"
	case strings.Contains(text, "HOPOS_API_NO_AUTH"):
		n.State = "parked — no api key set"
	case strings.Contains(text, "HOPOS_NET_UP"):
		n.State = "network up"
	}
	n.InsecND = strings.Contains(text, "HOPOS_API_INSECURE")
	return n
}
