The desktop tool for [HopOS](https://gethop.org) nodes: **burn** a card, **configure** it afterwards, and **find** your nodes on the network.

## Downloads

- **hop-imager-macos-universal.zip** — Apple Silicon and Intel in one binary, signed and notarized: unzip, move `HopOS Imager.app` to Applications, and it opens without a warning.
- **hop-imager-linux-amd64.tar.gz** — writing to a device needs root (`sudo ./hop-imager`) or membership of the `disk` group.
- **hop-imager-windows-amd64.zip** — writing needs administrator rights; the app offers to restart elevated when you hit Write.

`SHA256SUMS` covers every asset. The **rolling-release** tag always carries the newest build at a URL that never changes; numbered releases stay put.

## Notes

**One thing at a time.** The window opens on three buttons — BURN, CONFIGURE, FIND — each saying in one line what it does, and only the tools for the job you picked are on screen. Back is the arrow top left, or Escape. The 1-2-3 in the corner is the order a new node goes through, not a wizard: steps 2 and 3 stand on their own.

**The config editor keeps its colours while you type.** There is no view mode to leave: one field, always editable, recoloured on every keystroke — section headers, comments, keys, values, JSON job lines, and secrets in copper so an open config shows you where its keys are.

**The progress bar tells the truth.** Phases that can be counted (writing, verifying, downloading) fill it; phases that are one movement — like putting your kept config back — run it as a busy bar instead of leaving it at 100% while the tool is still working.

**Run the macOS build as the app, not as a loose binary.** macOS grants local-network access per app, and a bare binary has no identity to hang that permission on — every connection to a LAN peer then fails instantly with "no route to host", so FIND would see an empty network. The bundle asks once, and remembers.

**Writing verifies itself.** The card is read back and compared to the image byte for byte on the same open device, before anything can mount it. That is the only moment the comparison means something: verify later and you are comparing against bytes the operating system wrote itself the instant it mounted your fresh card.

**Windows writes the way Windows demands.** Before a single byte goes to `\\.\PhysicalDriveN`, every volume on that disk is locked and dismounted, and those handles are held open for the whole write — otherwise Windows remounts the volume mid-flash and lays its own filesystem over yours. Afterwards the locks are released and the disk re-reads its partition table.

**Configuring is always afterwards, and always raw.** Every HopOS image carries its config in a fixed-size window (`#HOPCFG1`) that this tool patches in place — no mount, so it works on a card your computer cannot mount, and on the LicheeRV Nano, where the config lives inside the kernel and the FIP checksums are fixed up for you.
