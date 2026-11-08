The desktop tool for [HopOS](https://gethop.org) nodes: **burn** a card, **configure** it afterwards, and **find** your nodes on the network.

## Downloads

- **hop-imager-macos-universal.zip** — Apple Silicon and Intel in one binary, signed and notarized: unzip, move `HopOS Imager.app` to Applications, and it opens without a warning.
- **hop-imager-linux-amd64.tar.gz** — writing to a device needs root (`sudo ./hop-imager`) or membership of the `disk` group.
- **hop-imager-windows-amd64.zip** — writing needs administrator rights; the app offers to restart elevated when you hit Write.

`SHA256SUMS` covers every asset. The **rolling-release** tag always carries the newest build at a URL that never changes; numbered releases stay put.

## Notes

**HopOS v3: the Rust generation.** HopOS is written in Rust now, and moved to the [hop-os](https://github.com/hop-os) organisation on GitHub, the same as this tool. BURN reads the v3 releases from [hop-os/HopOS](https://github.com/hop-os/HopOS): two types per board, *Headless* (no GUI linked) and *Headfull* (the GUI, plus the video codec and optical drives on the Radxa Orion O6N), and the board names v3 uses for the Radxa Zero 3E (`radxa`) and the Mac mini M4 (`apple`). The tool opens on Headless, the type every board has. The Go releases (v2) stay in the version list with their own three types, Desktop, Headless and Media, and switching between a v2 and a v3 version keeps your kind of card and your board.

**FIND reads a v3 node.** The Rust kernel and Hop print different console lines than the Go kernel did, and FIND now reads them: the name from Hop's `HOP_UP` line (or from the env the kernel hands Hop, before Hop is up), the architecture and the **HopOS version** from the runtime line, the GUI from `HOPOS_FB_CONSOLE`. A v3 console is a 32 KiB ring, so on a node that has run for hours, or that flipped its kernel, the boot is gone. FIND then asks Hop itself (`GET :8080/v1/agents`) for the name and version, and leaves out what nobody can tell it any more instead of guessing. The state is one of `agent up`, `hop starting`, `parked — no api key set` or `hop down`. **Web** opens what the node serves on port 80, *welcome* on a headfull node, and is enabled only when something answers there. Go nodes are still found. Note that in v3 the console port (`tcp/5555`) is open only with `hopos.console=1` or `hopos.insecure=1`. Both shipped configs set both, but a node you locked down is invisible to FIND.

**The config window is inside the kernel, on every board.** A v3 image carries its config in a 16 KiB `#HOPCFG1` window inside the kernel binary itself. CONFIGURE and *Keep this card's config* find it there, just as they found the 1 MiB window on a Go card. A kept config that does not fit the smaller window is not cut short: the card keeps the image's default, and your config stays in the backup. The editor also shows `hopos.lock.apikey` in copper as a secret.

**Stable or beta, and which version.** HopOS ships betas too, so BURN opens on a line that says what you are about to write: the channel as a coloured tag — `stable` in phosphor, `beta` in copper — with the version number behind it. That number is the selector: every HopOS release carrying card images is in the list, newest first, so going back to the version your other nodes run is one click. The tool starts on the newest **stable**; a beta is something you choose, not something that happens to you. `./hop-imager -releases` prints the same list without opening a window.

**The Mac mini M4 is in the board list.** HopOS builds a card image for it, so BURN calls it by its name — *Mac mini M4* — in its own place between the boards and the generic UEFI stick, instead of showing the bare asset id at the bottom. The port is headless-only for now, so under *Desktop* its button stays visible but disabled: that is a fact about the port, not a missing download.

**One thing at a time.** The window opens on three buttons — BURN, CONFIGURE, FIND — each saying in one line what it does, and only the tools for the job you picked are on screen. Back is the arrow top left, or Escape. The 1-2-3 in the corner is the order a new node goes through, not a wizard: steps 2 and 3 stand on their own.

**The config editor keeps its colours while you type.** There is no view mode to leave: one field, always editable, recoloured on every keystroke — section headers, comments, keys, values, JSON job lines, and secrets in copper so an open config shows you where its keys are.

**The progress bar tells the truth.** Phases that can be counted (writing, verifying, downloading) fill it; phases that are one movement — like putting your kept config back — run it as a busy bar instead of leaving it at 100% while the tool is still working.

**Run the macOS build as the app, not as a loose binary.** macOS grants local-network access per app, and a bare binary has no identity to hang that permission on — every connection to a LAN peer then fails instantly with "no route to host", so FIND would see an empty network. The bundle asks once, and remembers.

**Writing verifies itself.** The card is read back and compared to the image byte for byte on the same open device, before anything can mount it. That is the only moment the comparison means something: verify later and you are comparing against bytes the operating system wrote itself the instant it mounted your fresh card.

**Windows writes the way Windows demands.** Before a single byte goes to `\\.\PhysicalDriveN`, every volume on that disk is locked and dismounted, and those handles are held open for the whole write — otherwise Windows remounts the volume mid-flash and lays its own filesystem over yours. Afterwards the locks are released and the disk re-reads its partition table.

**Configuring is always afterwards, and always raw.** Every HopOS image carries its config in a fixed-size window (`#HOPCFG1`) that this tool patches in place — no mount, so it works on a card your computer cannot mount, and on every board's kernel, including the LicheeRV Nano's FIP, where the checksums are fixed up for you.
