# Plan 019: Ogg/ID3 correctness cluster — granule scan validation, packet lacing, ID3 unsync

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/ogg/container.go internal/mp3/id3v2.go internal/binary/reader.go internal/m4a/technical.go`
> On a mismatch with the excerpts below, STOP for the affected part (the four parts
> are independent — you may complete the unaffected ones).

## Status

- **Priority**: P3
- **Effort**: M (four small, independent fixes)
- **Risk**: MED (parts B and C touch framing/decoding of real-world files; corpus tests required)
- **Depends on**: 001
- **Category**: bug
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

Four independently-verified correctness gaps, batched because each is small and they share the
same regression infrastructure (plan 001's corpus + fuzzers):

- **(A) Ogg duration can read a granule from payload bytes.** The last-page search scans
  backward for the literal `OggS` and trusts the first hit — but page *payloads* can contain
  those four bytes, and payload bytes sit *after* their page header, so a backward scan finds
  them first. Result: garbage granule → wrong duration. No candidate validation is done at all.
- **(B) Ogg packet extraction ignores the segment lacing table.** `extractPackets` treats each
  page's whole `Data` as one packet, so when the comment and setup headers share a page (legal
  and common), packets are mis-framed; `len(packets) < 2` (`internal/ogg/parser.go:87-89`) then
  rejects a valid file.
- **(C) ID3v2 unsynchronization is never undone**, and frame-level compression/encryption/DLI
  flags are stored but ignored — tags written with unsync decode to corrupted text or a
  desynchronized frame walk.
- **(D) Two latent guards**: the `SafeReader.ReadAt` bound check `off+int64(len(b)) > sr.size`
  can overflow for huge `len(b)`; and `internal/m4a/technical.go` `parseStsd` reads
  channels/sample-rate at version-0 sample-entry offsets without checking the entry version
  (v1/v2 entries → wrong bytes). D's second half is an **investigate** item: confirm before fixing.

## Current state

**(A)** `internal/ogg/container.go:146-179` (verified):

```go
searchStart := fileSize - 65536
...
buf := make([]byte, searchSize)
if err := sr.ReadAt(buf, searchStart, "search region"); err != nil { return 0, err }
// Find last "OggS" marker
lastOggPos := int64(-1)
for i := len(buf) - 4; i >= 0; i-- {
	if buf[i] == 'O' && buf[i+1] == 'g' && buf[i+2] == 'g' && buf[i+3] == 'S' {
		lastOggPos = searchStart + int64(i)
		break
	}
}
...
granule, err := binary.ReadLE[uint64](sr, lastOggPos+6, "granule position")
```

Ogg page header layout (for validation): bytes 0-3 `OggS`; byte 4 version (must be 0); byte 5
header-type flags; bytes 6-13 granule (LE); 14-17 serial; 18-21 sequence; 22-25 CRC; byte 26
segment count N; bytes 27..27+N lacing values; then payload of sum(lacing) bytes.

**(B)** `internal/ogg/container.go:59-101` `readPage` (verified) reads the segment table, sums it
into `dataSize`, and **discards the lacing values** (the `Page` struct keeps only `Data`).
`extractPackets` (:113-140, verified) splits only on the continuation flag:

```go
for _, page := range pages {
	if page.HeaderType&0x01 != 0 && len(currentPacket) > 0 {
		currentPacket = append(currentPacket, page.Data...)
	} else {
		if len(currentPacket) > 0 { packets = append(packets, currentPacket) }
		currentPacket = make([]byte, len(page.Data)); copy(currentPacket, page.Data)
	}
}
```

Spec behavior it must implement: within a page, packets end at each lacing value < 255; a
packet continues across pages when its last lacing value is 255 (and the next page sets the
continuation flag). The doc comment on `extractPackets` (:104-112) already *claims* exactly
this — the code just doesn't do it.

**(C)** `internal/mp3/id3v2.go` (verified): `parseID3v2Header` (:100-104) captures `Flags`
(header unsync = bit 0x80