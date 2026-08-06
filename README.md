# hop-imager

The desktop tool for [HopOS](https://gethop.org) nodes: **burn** a card,
**configure** it, and **find** your nodes on the network. One window, three
tabs, no terminal.

```
   (\(\
   ( -.-)     HopOS
   o_(")(")   --------------
              the Go-only OS
```

## What it does

**BURN** — picks up the latest HopOS release from GitHub, downloads the card
image for your board and writes it. Choose the **type** first (Desktop, which
boots into the GUI, or Headless, which is API + network console only), then
your **board**. *Verify* reads the card back and compares it to the image byte
for byte.

**CONFIGURE** — edits the node config on a card that is **already written**.
That is the point: configuring is something you do afterwards, as often as you
like. Put the card back in your reader, load, edit, save, boot.

**FIND** — answers "where is that thing on my network?". Every HopOS node
serves its console on `tcp/5555`, so one connect both identifies a node and
reveals its name, architecture, type and state. Click a node to read its boot
log. Nodes appear in the list while the scan is still running.

## Why the config edit always works

HopOS images carry their config as a fixed-size **config window**: a magic
first line, the config, and `#`-comment padding.

```
#HOPCFG1 window=1048576 len=0000005179
hopos.node=hop-1
hopos.console=5555
...
################################################################   (padding)
```

To every config parser that is just a config file with comments in it. To this
tool it is a block of bytes that can be rewritten **in place** — so editing
works before flashing (on the image) and after (on the card), without mounting
anything, and regardless of whether your OS can mount the boot partition at
all. On the LicheeRV Nano, where the config lives inside the kernel, the tool
also fixes the Sophgo FIP checksums that guard it.

The same format is written by HopOS's own `image/mkcard -cfgwindow` and can be
edited from the command line with `image/hopcfg` in the
[HopOS repository](https://github.com/xinix00/HopOS).

## Privileges

Writing a card and editing its config need raw access to the device. The tool
asks for that **per operation** with the normal system password prompt, then
unmounts the card and works on the raw device. The GUI itself stays an ordinary
user process — you never have to start it with `sudo`.

## Build and run

```sh
go build .
./hop-imager
```

Command line, no GUI needed:

```sh
./hop-imager -scan      # list HopOS nodes on this network
```

## Platform support

macOS works today. Windows and Linux need two files filled in —
`internal/disk` (enumerate and unmount removable drives) and `internal/priv`
(escalate: UAC or pkexec/sudo). Everything above those two is already
platform-neutral.

## License

MIT — see [LICENSE](LICENSE).
