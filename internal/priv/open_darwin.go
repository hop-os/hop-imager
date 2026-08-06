//go:build darwin

package priv

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// OpenDevice opent een rauw blok-device met de rechten die macOS ervoor eist.
//
// WAAROM NIET GEWOON os.OpenFile ALS ROOT (en dit is gemeten, 06-08): root
// zijn is niet genoeg. Zelfs Apple's eigen /bin/dd krijgt als uid 0
// "Operation not permitted" op /dev/rdisk4 — dat is EPERM van TCC, niet EACCES
// van de bestandsrechten. macOS geeft rauwe schijftoegang alleen aan processen
// wier verantwoordelijke proces Volledige Schijftoegang heeft (Terminal.app
// heeft dat vaak, een via osascript gestarte binary nooit).
//
// De weg die Apple daarvoor open laat is /usr/libexec/authopen: een
// ondertekende setuid-helper die zélf om toestemming vraagt (het bekende
// autorisatie-dialoog) en het geopende bestand als FILE DESCRIPTOR teruggeeft
// over een socket. Die fd is een gewone *os.File — seekbaar, dus precies wat
// het config-venster nodig heeft, en snel genoeg om een image mee te schrijven.
//
// Eén prompt per open. Dat is dezelfde afspraak als hiervoor: de gebruiker
// ziet het systeemdialoog op het moment dat de tool iets aan de kaart doet.
func OpenDevice(path string, write bool) (*os.File, error) {
	flags := syscall.O_RDONLY
	args := []string{"-stdoutpipe"}
	if write {
		flags = syscall.O_RDWR
		args = append(args, "-w")
	}
	args = append(args, "-o", strconv.Itoa(flags), path)

	// Socketpair: authopen schrijft de fd als ancillary data (SCM_RIGHTS) naar
	// zijn stdout, dus die stdout MOET een unix-socket zijn.
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("socketpair: %v", err)
	}
	ours := os.NewFile(uintptr(fds[0]), "authopen-parent")
	theirs := os.NewFile(uintptr(fds[1]), "authopen-child")
	defer ours.Close()

	var stderr strings.Builder
	cmd := exec.Command("/usr/libexec/authopen", args...)
	cmd.Stdout = theirs
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		theirs.Close()
		return nil, fmt.Errorf("authopen: %v", err)
	}
	theirs.Close() // alleen authopen houdt de schrijfkant nog vast

	// De fd ophalen. Het databyte dat authopen meestuurt doet niet mee; het
	// gaat om het controlebericht.
	data := make([]byte, 16)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, rerr := syscall.Recvmsg(int(ours.Fd()), data, oob, 0)
	waitErr := cmd.Wait()
	if rerr != nil {
		return nil, fmt.Errorf("authopen: %v", rerr)
	}
	if oobn == 0 {
		// Geen fd: de gebruiker klikte weg, of authopen mocht het niet.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && waitErr != nil {
			msg = waitErr.Error()
		}
		if msg == "" {
			msg = "no descriptor returned"
		}
		return nil, fmt.Errorf("authorization for %s failed: %s", path, msg)
	}
	_ = n
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, fmt.Errorf("authopen control message: %v", err)
	}
	for _, m := range msgs {
		got, err := syscall.ParseUnixRights(&m)
		if err != nil || len(got) == 0 {
			continue
		}
		return os.NewFile(uintptr(got[0]), path), nil
	}
	return nil, fmt.Errorf("authopen returned no file descriptor for %s", path)
}

// NeedsElevation/Elevate bestaan alleen voor Windows; op macOS vraagt authopen
// per bewerking om toestemming en hoeft het proces nooit verhoogd te draaien.
func NeedsElevation() bool { return false }
func Elevate() error       { return nil }
