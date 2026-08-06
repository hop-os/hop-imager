The desktop tool for [HopOS](https://gethop.org) nodes: **burn** a card, **configure** it afterwards, and **find** your nodes on the network.

## Downloads

- **hop-imager-macos-arm64.zip** — Apple Silicon. Unzip and move `HopOS Imager.app` to Applications.
- **hop-imager-macos-amd64.zip** — Intel Macs.
- **hop-imager-linux-amd64.tar.gz** — writing to a device needs root (`sudo ./hop-imager`) or membership of the `disk` group.
- **hop-imager-windows-amd64.zip** — writing needs administrator rights; the app offers to restart elevated when you hit Write.

`SHA256SUMS` covers every asset. The **rolling-release** tag always carries the newest build at a URL that never changes; numbered releases stay put.

## Notes

**Run the macOS build as the app, not as a loose binary.** macOS grants local-network access per app, and a bare binary has no identity to hang that permission on — every connection to a LAN peer then fails instantly with "no route to host", so FIND would see an empty network. The bundle asks once, and the app tells you if it was refused instead of pretending nothing is there.

**Writing verifies itself.** The card is read back and compared to the image byte for byte on the same open device, before anything can mount it. That is the only moment the comparison means something: verify later and you are comparing against bytes the operating system wrote itself the instant it mounted your fresh card.

**Windows writes the way Windows demands.** Before a single byte goes to `\\.\PhysicalDriveN`, every volume on that disk is locked and dismounted, and those handles are held open for the whole write — otherwise Windows remounts the volume mid-flash and lays its own filesystem over yours. Afterwards the locks are released and the disk re-reads its partition table.

**Configuring is always afterwards, and always raw.** Every HopOS image carries its config in a fixed-size window (`#HOPCFG1`) that this tool patches in place — no mount, so it works on a card your computer cannot mount, and on the LicheeRV Nano, where the config lives inside the kernel and the FIP checksums are fixed up for you.
