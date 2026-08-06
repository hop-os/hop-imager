package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tmpKept zet de back-upmap in de testmap: nooit in de echte config-map van de
// gebruiker schrijven (os.UserConfigDir leest op macOS geen omgeving).
func tmpKept(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := keptDir
	keptDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { keptDir = old })
}

// fakeImage bouwt een miniatuur-image met een echt config-venster erin: het
// formaat uit cfgwin (magic-kopregel, config, '#'-padding tot een 512-voud) op
// een sector-uitgelijnd offset. Klein genoeg om snel te zijn, echt genoeg om
// door dezelfde code gevonden te worden als een kaart van 8GB.
func fakeImage(t *testing.T, name, cfg string, winSize int) string {
	t.Helper()
	const off = 0x1000
	img := make([]byte, 1<<20)
	for i := range img {
		img[i] = byte(i % 251) // niet-nul vulling: dan valt een fout écht op
	}
	if !strings.HasSuffix(cfg, "\n") {
		cfg += "\n"
	}
	head := fmt.Sprintf("#HOPCFG1 window=%d len=%010d\n", winSize, len(cfg))
	win := make([]byte, 0, winSize)
	win = append(win, head...)
	win = append(win, cfg...)
	for len(win) < winSize {
		n := 65
		if r := winSize - len(win); r < n {
			n = r
		}
		line := strings.Repeat("#", n-1) + "\n"
		win = append(win, line[:n]...)
	}
	copy(img[off:], win)
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, img, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func windowConfig(t *testing.T, path string) string {
	t.Helper()
	b, _, err := opCfgRead(path)
	if err != nil {
		t.Fatalf("venster lezen uit %s: %v", path, err)
	}
	return string(b)
}

// TestKeepConfigOverleeftEenFlash is de kern van de feature: de node houdt zijn
// naam, sleutels en jobs terwijl er nieuwe code op de kaart komt.
func TestKeepConfigOverleeftEenFlash(t *testing.T) {
	keepEligible = func(string) bool { return true } // doe alsof dit een kaart is
	t.Cleanup(func() { keepEligible = isDevice })
	tmpKept(t)

	card := fakeImage(t, "card.img", "hopos.node=hop-3\nhopos.apikey=geheim\nhopos.insecure=1\n", 4096)
	fresh := fakeImage(t, "new.img", "hopos.node=hop-1\nhopos.insecure=1\nhopos.console=5555\n", 4096)

	res, err := opWrite(fresh, card, true, func(string) {}, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.MismatchAt != -1 {
		t.Fatalf("image niet byte-exact geland: verschil op %#x", res.MismatchAt)
	}
	if !res.Kept {
		t.Fatal("config niet teruggezet")
	}
	if res.KeptNode != "hop-3" {
		t.Errorf("KeptNode = %q, wil hop-3", res.KeptNode)
	}
	got := windowConfig(t, card)
	if !strings.Contains(got, "hopos.node=hop-3") || !strings.Contains(got, "hopos.apikey=geheim") {
		t.Errorf("de kaart draagt niet de bewaarde config:\n%s", got)
	}
	if strings.Contains(got, "hopos.node=hop-1") {
		t.Error("de default-config van het image staat er nog")
	}
	// Wat de nieuwe default zet en de bewaarde config niet heeft, moet gemeld
	// worden — niet stil samengevoegd.
	if len(res.NewKeys) != 1 || res.NewKeys[0] != "hopos.console" {
		t.Errorf("NewKeys = %v, wil [hopos.console]", res.NewKeys)
	}
	// De back-up is het vangnet: hij moet er zijn, met de geheimen erin, en
	// alleen voor de eigenaar leesbaar.
	if res.Backup == "" {
		t.Fatal("geen back-up geschreven")
	}
	b, err := os.ReadFile(res.Backup)
	if err != nil || !strings.Contains(string(b), "hopos.apikey=geheim") {
		t.Errorf("back-up %s klopt niet: %v", res.Backup, err)
	}
	if st, err := os.Stat(res.Backup); err == nil && st.Mode().Perm() != 0600 {
		t.Errorf("back-up staat op %v, moet 0600 zijn — er staan geheimen in", st.Mode().Perm())
	}
}

// TestZonderKeepKrijgtDeDefault: het vinkje uit betekent echt de default van het
// image, en dan hoort er ook geen back-up te zijn.
func TestZonderKeepKrijgtDeDefault(t *testing.T) {
	keepEligible = func(string) bool { return true }
	t.Cleanup(func() { keepEligible = isDevice })
	tmpKept(t)

	card := fakeImage(t, "card.img", "hopos.node=hop-3\n", 4096)
	fresh := fakeImage(t, "new.img", "hopos.node=hop-1\n", 4096)

	res, err := opWrite(fresh, card, false, func(string) {}, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kept || res.Backup != "" {
		t.Errorf("keep stond uit, maar Kept=%v backup=%q", res.Kept, res.Backup)
	}
	if got := windowConfig(t, card); !strings.Contains(got, "hopos.node=hop-1") {
		t.Errorf("de kaart draagt niet de default:\n%s", got)
	}
}

// TestGeenVensterOpDeKaart: een verse of vreemde kaart heeft niets te bewaren.
// Dat is geen fout — de flash gaat door met de default.
func TestGeenVensterOpDeKaart(t *testing.T) {
	keepEligible = func(string) bool { return true }
	t.Cleanup(func() { keepEligible = isDevice })
	tmpKept(t)

	blank := filepath.Join(t.TempDir(), "blank.img")
	if err := os.WriteFile(blank, make([]byte, 1<<20), 0644); err != nil {
		t.Fatal(err)
	}
	fresh := fakeImage(t, "new.img", "hopos.node=hop-1\n", 4096)

	res, err := opWrite(fresh, blank, true, func(string) {}, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.MismatchAt != -1 || res.Kept || res.Backup != "" {
		t.Errorf("verse kaart: %+v", res)
	}
}

// TestDesktopConfigOpHeadlessImageWaarschuwt: die combinatie zaait een
// display-job die het glas nooit krijgt, en dat moet de tool zeggen.
func TestDesktopConfigOpHeadlessImageWaarschuwt(t *testing.T) {
	keepEligible = func(string) bool { return true }
	t.Cleanup(func() { keepEligible = isDevice })
	tmpKept(t)

	card := fakeImage(t, "card.img", `hopos.node=hop-3
hopos.init[]={"name":"display","driver":"hop","env":{"FB":"1"}}
`, 4096)
	fresh := fakeImage(t, "new.img", "hopos.node=hop-1\nhopos.init[]={\"name\":\"welcome\"}\n", 4096)

	res, err := opWrite(fresh, card, true, func(string) {}, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Warning, "headless") {
		t.Errorf("geen waarschuwing over headless: %q", res.Warning)
	}
}
