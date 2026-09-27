// Package release praat met GitHub: de releases van xinix00/HopOS uitlezen —
// stable én beta — en een gekozen kaart-image downloaden (met voortgang, en
// .gz transparant uitgepakt — het venster-patchen werkt op de rauwe bytes).
package release

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Repo is de bron van de images; één plek, zodat een fork hem kan verzetten.
const Repo = "xinix00/HopOS"

// De twee kanalen, in het woord dat de tool toont. GitHub kent alleen de vlag
// "prerelease"; stable en beta is hoe een mens het noemt.
const (
	Stable = "stable"
	Beta   = "beta"
)

type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// Release is één release van HopOS: de tag, of het een voorproefje is, en wat
// eraan hangt.
type Release struct {
	Tag        string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// Channel geeft het kanaal als woord: dat is wat de tool bovenin toont.
func (r Release) Channel() string {
	if r.Prerelease {
		return Beta
	}
	return Stable
}

var client = &http.Client{Timeout: 30 * time.Second}

// List geeft de releases van nieuw naar oud, prereleases inbegrepen — die zijn
// juist de reden dat er iets te kiezen valt. Drafts blijven eruit: die bestaan
// alleen voor wie ze schreef, en hun assets zijn niet publiek te downloaden.
//
// Eén pagina van honderd is de horizon. Dat is geen willekeurige grens: verder
// terug staan alleen de tags van vóór de kaart-images, en die vallen in
// WithImages tóch af.
func List(repo string) ([]Release, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hop-imager")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API: %s", resp.Status)
	}
	var all []Release
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, err
	}
	var out []Release
	for _, r := range all {
		if !r.Draft {
			out = append(out, r)
		}
	}
	return out, nil
}

// WithImages houdt de releases over die ook echt een kaart-image dragen. De
// eerste HopOS-tags hingen alleen broncode aan de release, en een versie die
// je niet kunt branden hoort niet in een keuzelijst van deze tool.
func WithImages(rels []Release) []Release {
	var out []Release
	for _, r := range rels {
		if len(Images(r.Assets)) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Pick kiest de release die de tool uit zichzelf laat zien: de nieuwste
// stable. Een beta is iets wat je kiest, niet iets wat je overkomt — pas als
// er geen enkele stable in de lijst staat valt hij terug op de nieuwste beta,
// want dan is dát het enige dat er te branden valt.
func Pick(rels []Release) (Release, bool) {
	for _, r := range rels {
		if !r.Prerelease {
			return r, true
		}
	}
	if len(rels) > 0 {
		return rels[0], true
	}
	return Release{}, false
}

// Images filtert de assets op flashbare kaart-images.
func Images(assets []Asset) []Asset {
	var out []Asset
	for _, a := range assets {
		if strings.HasSuffix(a.Name, ".img") || strings.HasSuffix(a.Name, ".img.gz") {
			out = append(out, a)
		}
	}
	return out
}

// SplitFlavor splitst een asset-naam in de basis en de smaak — release.sh-
// conventie: hopos-<board>[-headless|-media].img[.gz], geen suffix = desktop
// (gui). Ná download verifieert de UI dit uit het config-venster zelf
// (main.setImage); alleen media is daar niet te zien, want die draagt dezelfde
// config als desktop.
func SplitFlavor(name string) (base, flavor string) {
	base = strings.TrimSuffix(strings.TrimSuffix(name, ".gz"), ".img")
	for _, fl := range []string{"headless", "media"} {
		if strings.HasSuffix(base, "-"+fl) {
			return strings.TrimSuffix(base, "-"+fl), fl
		}
	}
	return base, "desktop"
}

// Download haalt een asset naar dir en geeft het lokale pad terug. Een
// .gz-asset wordt tijdens het downloaden uitgepakt: de tool werkt overal op
// de rauwe image-bytes. Bestaat het bestand al met de juiste maat (en is het
// geen .gz), dan is de download een no-op — kaarten flashen is herhaalwerk.
func Download(a Asset, dir string, progress func(done, total int64)) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(a.Name, ".gz")
	dst := filepath.Join(dir, name)
	if name == a.Name {
		if st, err := os.Stat(dst); err == nil && st.Size() == a.Size {
			progress(a.Size, a.Size)
			return dst, nil
		}
	}
	req, err := http.NewRequest("GET", a.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "hop-imager")
	// Geen client-timeout hier: een image-download mag minuten duren.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download %s: %s", a.Name, resp.Status)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	var src io.Reader = &progressReader{r: resp.Body, total: total, cb: progress}
	if strings.HasSuffix(a.Name, ".gz") {
		if src, err = gzip.NewReader(src); err != nil {
			return "", err
		}
	}
	out, err := os.Create(dst + ".part")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		os.Remove(dst + ".part")
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return dst, os.Rename(dst+".part", dst)
}

// progressReader rapporteert gedownloade bytes (vóór eventuele decompressie —
// de voortgang volgt het netwerk, dat is wat de gebruiker ziet).
type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	cb    func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.cb != nil {
		p.cb(p.done, p.total)
	}
	return n, err
}
