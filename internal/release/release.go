// Package release praat met GitHub: de laatste release van xinix00/HopOS
// uitlezen en een gekozen kaart-image downloaden (met voortgang, en .gz
// transparant uitgepakt — het venster-patchen werkt op de rauwe bytes).
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

type Asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

var client = &http.Client{Timeout: 30 * time.Second}

// Latest geeft de tag en de assets van de laatste release.
func Latest(repo string) (string, []Asset, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "hop-imager")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("GitHub API: %s", resp.Status)
	}
	var rel struct {
		Tag    string  `json:"tag_name"`
		Assets []Asset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", nil, err
	}
	return rel.Tag, rel.Assets, nil
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

// Flavor leidt de smaak af uit de asset-naam — release.sh-conventie:
// hopos-<board>[-headless].img.gz, geen suffix = desktop (gui). Ná download
// verifieert de UI dit uit het config-venster zelf (main.flavorOf).
func Flavor(name string) string {
	if strings.Contains(name, "-headless") {
		return "headless"
	}
	return "desktop"
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
