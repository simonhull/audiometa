# Plan 030: Detect AAC profile correctly and honor M4B compatible brands

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/codecs.go internal/m4a/technical.go internal/m4a/parser.go internal/types/format.go file.go format_test.go`
> If any of these changed, compare the "Current state" excerpts against the live code. A mismatch
> in Part A files (`codecs.go`, `technical.go`) blocks only Part A, and a mismatch in Part B files
> blocks only Part B. The two parts are independent.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (Part A fills a field that is always empty today; Part B accepts strictly more files and reclassifies some M4A as M4B)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

**A. `CodecProfile` is never set for AAC.** The esds parser returns the DecoderConfigDescriptor's
`objectTypeIndication` byte, which is `0x40` ("MPEG-4 Audio") for every AAC file. It does not return
the AudioSpecificConfig's `audioObjectType`. `0x40` (64) is not in the `aacProfiles` map, so
HE-AAC, HE-AAC v2, and AAC-LC all report `CodecProfile == ""` and `CodecDescription == "AAC"`.
`types.AudioInfo` already has consumers of the profile (`internal/types/audio.go:197,214`), and they
never fire for M4A. Separately, the esds search reads a fixed 256 bytes from the sample entry. When
the sample entry sits within 256 bytes of end-of-file (an untagged file with `moov` last), that read
fails and the profile is silently skipped.

**B. M4B files are misreported as M4A, and valid MP4 audio is rejected.** `types.DetectFormat`
looks only at the ftyp *major* brand and accepts only `M4B `, `M4A `, `mp42`, and `isom`. Files
whose major brand is `mp41`, `iso2`, or `M4P ` are rejected with `UnsupportedFormatError`, even
though the M4A parser handles them. Many encoders (ffmpeg, some audiobook tools) write
`isom`/`mp42` as the major brand and list `M4B ` only as a *compatible* brand. `m4a.detectM4Format`
does scan compatible brands, but `openReader` overwrites its answer with `DetectFormat`'s, so those
audiobooks surface as `FormatM4A`. The audiobook server keys on M4B.

## Current state

### Part A: AAC profile

`internal/m4a/codecs.go:31-41` (verified). The profile map is keyed by audioObjectType:

```go
var aacProfiles = map[uint8]string{
	1:  "AAC Main",
	2:  "AAC-LC",
	3:  "AAC-SSR",
	4:  "AAC-LTP",
	5:  "HE-AAC",
	6:  "AAC Scalable",
	29: "HE-AAC v2",
	42: "xHE-AAC",
}
```

`internal/m4a/codecs.go:51-71`. `parseCodecDetails(sr, sampleEntryOffset int64, codec string, file)`
calls `parseAACProfile(sr, sampleEntryOffset)` for `mp4a`. If the result is non-empty, it sets
`CodecProfile` and, when that is not `"AAC-LC"`, also `CodecDescription = profile`.

`internal/m4a/codecs.go:73-119` (verified, abridged):

```go
func parseAACProfile(sr *binary.SafeReader, sampleEntryOffset int64) (string, error) {
	// Search for "esds" within sample entry
	searchBuf := make([]byte, 256)                                     // :76 fixed size
	if err := sr.ReadAt(searchBuf, sampleEntryOffset, "esds search buffer"); err != nil {
		return "", err                                                  // fails if < 256 bytes to EOF
	}
	... finds "esds", esdsOffset = sampleEntryOffset + i - 4 (start of the esds size field)
	esdsSize, err := binary.Read[uint32](sr, esdsOffset, "esds size")
	if err != nil || esdsSize < 12 || esdsSize > 1024 { return "", nil }
	esdsDataSize := int(esdsSize) - 12                                  // skip size, "esds", version+flags
	...
	esdsData := make([]byte, esdsDataSize)
	if err := sr.ReadAt(esdsData, esdsOffset+12, "esds data"); err != nil { return "", err }
	audioObjectType := parseESDescriptors(esdsData)
	...lookup in aacProfiles...
}
```

`internal/m4a/codecs.go:121-166` (verified). The bug is at `:153-159`:

```go
	for pos < len(data) {
		if data[pos] == 0x03 {
			pos++
			size := readSize()
			if size < 0 {
				return 0
			}
			pos += 3                                     // ES_ID(2) + flags(1); ignores flag-dependent fields

			if pos < len(data) && data[pos] == 0x04 {
				pos++
				dcSize := readSize()
				if dcSize < 0 || pos >= len(data) {
					return 0
				}
				return data[pos]                         // <-- objectTypeIndication (0x40), NOT audioObjectType
			}
		}
		pos++
	}
```

Correct layout (ISO/IEC 14496-1 §7.2.6.5–7.2.6.7, 14496-3 §1.6.2.1). The esds payload starts
directly with:
- `0x03` ES_Descriptor, then size (1–4 bytes, 7 bits each, high bit = "more"), then ES_ID(2) and
  flags(1). If `flags&0x80` (streamDependenceFlag), skip 2 more bytes. If `flags&0x40` (URL_Flag),
  skip a 1-byte length plus that many bytes. If `flags&0x20` (OCRstreamFlag), skip 2 more bytes.
- `0x04` DecoderConfigDescriptor, then size, then 13 fixed bytes: objectTypeIndication(1),
  streamType/upStream/reserved(1), bufferSizeDB(3), maxBitrate(4), avgBitrate(4).
- `0x05` DecoderSpecificInfo, then size, then **AudioSpecificConfig**. Its top 5 bits are
  `audioObjectType`. The value 31 is an escape: `32 + next 6 bits`.

`internal/m4a/technical.go:176-199` (verified). The sample entry size is read and then discarded:

```go
	_, err = binary.Read[uint32](sr, offset, "stsd entry size")     // :182
	if err != nil {
		return err
	}
	offset += 4

	// Read format (codec type) - 4 bytes
	formatBytes := make([]byte, 4)
	if err := sr.ReadAt(formatBytes, offset, "stsd format"); err != nil {
		return err
	}
	offset += 4

	codec := string(formatBytes)
	file.Audio.Codec = codec

	// Parse enhanced codec details (non-fatal if it fails)
	_ = parseCodecDetails(sr, offset-8, codec, file)                // :199, offset-8 = sample entry start
```

`parseCodecDetails` has exactly one caller (this one). `parseAACProfile` is called only from
`parseCodecDetails`.

### Part B: brand detection

`internal/types/format.go:144-206` (verified, abridged). It reads the ftyp size and type at offsets
0 and 4, requires type `ftyp` and size ≥ 16, then:

```go
	// Read major brand (next 4 bytes)
	majorBrand, err := binary.Read[uint32](sr, 8, "major brand")
	if err != nil {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}

	// Check for M4B brand (0x4D344220 = "M4B ")
	m4bMagic := uint32(0x4D344220)
	if majorBrand == m4bMagic {
		return FormatM4B, nil
	}
	m4aMagic := uint32(0x4D344120)
	mp42Magic := uint32(0x6D703432)
	isomMagic := uint32(0x69736F6D)

	if majorBrand == m4aMagic || majorBrand == mp42Magic || majorBrand == isomMagic {
		return FormatM4A, nil
	}

	// Unsupported brand
	return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "unsupported file brand"}
}
```

`DetectFormat` carries `//nolint:gocyclo` on its signature line (`:67`). ftyp layout: size(4),
`ftyp`(4), major brand(4), minor version(4), compatible brands (4 bytes each) up to `size`.

`internal/m4a/parser.go:15-54`. `detectM4Format(sr, size)` re-finds ftyp via `findAtom`. It returns
M4B for major `M4B ` or for any compatible `M4B `, and M4A otherwise, including for unknown brands.
`Parse` uses it at `:66` (`format := detectM4Format(sr, size)`) to set `file.Format`.

`file.go:130-154`. `openReader` calls `types.DetectFormat`, then `parser.Parse`, then **overwrites**
`file.Format = format` (`:154`). The public `audiometa.DetectFormat` (`format.go:28`) is a thin
wrapper around `types.DetectFormat`.

Existing tests that pin behavior and must keep passing:
- `format_test.go` (package `audiometa`, repo root): `TestDetectFormat_M4B/_M4A/_MP42`,
  `TestDetectFormat_UnsupportedBrand` (brand `XXXX` with compatible `XXXX XXXX` must still error),
  and `createMockM4B(brand)` at `:11`.
- `internal/m4a/parser_test.go`: `TestParse_Success` (M4B ftyp → `FormatM4B`) and
  `TestParse_UnknownBrand` (`:124`; an ftyp with brand `XXXX` passed straight to `Parse` must yield
  `FormatM4A`, not an error).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Part A tests | `go test ./internal/m4a/ -run 'ESDescriptors\|AACProfile\|ParseTechnicalInfo' -v` | all PASS |
| Part B tests | `go test . ./internal/types/ ./internal/m4a/ -run 'DetectFormat\|Parse_' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Lint **must** go through `mise exec --`. A stale global golangci-lint 2.11.4 on PATH gives false
typecheck errors; the pinned version is 2.15.0. `go.mod` says `go 1.26.0`. The built-ins
`min`/`max` and the `slices` package are fine to use.

## Scope

**In scope**:
- Part A: `internal/m4a/codecs.go`, `internal/m4a/technical.go` (only the `:182` entry-size read and
  the `:199` call), `internal/m4a/esds_test.go` (create)
- Part B: `internal/types/format.go`, `internal/m4a/parser.go` (only `detectM4Format` and its call at
  `:66`), `format_test.go` (append tests)

**Out of scope**:
- Choosing the audio `trak` by `hdlr`, and stsd sample-entry version 1/2 offsets: that is plan
  `019-container-correctness.md`, part D. Do not touch `parseStsd`'s offset arithmetic or the
  `findAtom(..., atomTrak)` selection.
- Detecting **implicitly** signalled HE-AAC / HE-AAC v2 (AOT 2 in the AudioSpecificConfig with SBR/PS
  found only by decoding audio, or via a trailing sync-extension `0x2b7`). This plan reports exactly
  what the AudioSpecificConfig's leading audioObjectType says. Implicit HE-AAC will correctly read as
  `AAC-LC`. Parsing the backward-compatible sync extension is a possible follow-up, not this plan.
- `codecNames`, and adding new entries to `aacProfiles`.
- Removing `.m4p` or other entries from `Format.Extensions()`.
- `file.go`: after Part B, its overwrite is correct as-is, since there is one owner of the decision.

## Git workflow

- Branch: `advisor/030-m4a-codec-and-brand-detection`
- Two commits, gitmoji single-line, no body, no attribution. Suggested:
  `🐛 Read AAC profile from AudioSpecificConfig` and `🐛 Honor compatible ftyp brands in format detection`.
- Do NOT push or open a PR.

## Steps

### Step A1: Failing tests for the esds parser

Create `internal/m4a/esds_test.go`:

```go
package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// esdsPayload builds the descriptor bytes that follow an esds atom's
// version+flags: ES_Descriptor(0x03) → DecoderConfigDescriptor(0x04, OTI 0x40)
// → DecoderSpecificInfo(0x05) holding asc, then SLConfigDescriptor(0x06).
func esdsPayload(esFlags byte, asc []byte) []byte {
	dsi := append([]byte{0x05, byte(len(asc))}, asc...)
	dcd := append([]byte{0x04, byte(13 + len(dsi)),
		0x40,             // objectTypeIndication: MPEG-4 Audio
		0x15,             // streamType audio, upStream 0, reserved 1
		0x00, 0x00, 0x00, // bufferSizeDB
		0x00, 0x01, 0xF4, 0x00, // maxBitrate
		0x00, 0x01, 0xF4, 0x00, // avgBitrate
	}, dsi...)
	esBody := []byte{0x00, 0x01, esFlags} // ES_ID, flags
	if esFlags&0x80 != 0 {
		esBody = append(esBody, 0x00, 0x02) // dependsOn_ES_ID
	}
	esBody = append(esBody, dcd...)
	esBody = append(esBody, 0x06, 0x01, 0x02) // SLConfigDescriptor
	return append([]byte{0x03, byte(len(esBody))}, esBody...)
}

// mp4aSampleEntry builds a version-0 mp4a sample entry with a child esds atom.
func mp4aSampleEntry(esds []byte) []byte {
	body := &bytes.Buffer{}
	body.Write(make([]byte, 6))                      // reserved
	binary.Write(body, binary.BigEndian, uint16(1))  // data reference index
	body.Write(make([]byte, 8))                      // version, revision, vendor
	binary.Write(body, binary.BigEndian, uint16(2))  // channels
	binary.Write(body, binary.BigEndian, uint16(16)) // sample size
	body.Write(make([]byte, 4))                      // compression ID, packet size
	binary.Write(body, binary.BigEndian, uint32(44100<<16))
	body.Write(createMockAtom("esds", append([]byte{0, 0, 0, 0}, esds...)))
	return createMockAtom("mp4a", body.Bytes())
}

func TestParseESDescriptors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint8
	}{
		{"AAC-LC", esdsPayload(0, []byte{0x12, 0x10}), 2},
		{"HE-AAC explicit", esdsPayload(0, []byte{0x2B, 0x92, 0x08, 0x00}), 5},
		{"escaped AOT 42", esdsPayload(0, []byte{0xF9, 0x40}), 42},
		{"streamDependenceFlag", esdsPayload(0x80, []byte{0x12, 0x10}), 2},
		{"truncated", esdsPayload(0, []byte{0x12, 0x10})[:10], 0},
		{"empty", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseESDescriptors(tt.data); got != tt.want {
				t.Errorf("parseESDescriptors() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseCodecDetails_AACProfile(t *testing.T) {
	tests := []struct {
		name        string
		asc         []byte
		wantProfile string
		wantDesc    string
	}{
		{"AAC-LC", []byte{0x12, 0x10}, "AAC-LC", "AAC"},
		{"HE-AAC", []byte{0x2B, 0x92, 0x08, 0x00}, "HE-AAC", "HE-AAC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The entry is the whole file, so fewer than 256 bytes follow it.
			entry := mp4aSampleEntry(esdsPayload(0, tt.asc))
			sr := audiobinary.NewSafeReader(bytes.NewReader(entry), int64(len(entry)), "test.m4a")
			file := &types.File{}
			if err := parseCodecDetails(sr, 0, uint32(len(entry)), "mp4a", file); err != nil {
				t.Fatalf("parseCodecDetails: %v", err)
			}
			if file.Audio.CodecProfile != tt.wantProfile {
				t.Errorf("CodecProfile = %q, want %q", file.Audio.CodecProfile, tt.wantProfile)
			}
			if file.Audio.CodecDescription != tt.wantDesc {
				t.Errorf("CodecDescription = %q, want %q", file.Audio.CodecDescription, tt.wantDesc)
			}
		})
	}
}
```

Byte meanings, so you can check them. `0x12 0x10` = AOT 2 (AAC-LC), 44.1 kHz, stereo.
`0x2B 0x92 0x08 0x00` = AOT 5 (explicit HE-AAC). `0xF9 0x40` = escape 31, then 6 bits `001010` =
32+10 = 42.

**Verify**: `go vet ./internal/m4a/` fails only because `parseCodecDetails` takes 4 arguments, not
5. That is expected until Step A3. To see the AOT bug itself, temporarily delete
`TestParseCodecDetails_AACProfile` and run `go test ./internal/m4a/ -run ESDescriptors -v`: it
reports `parseESDescriptors() = 64, want 2` and similar (confirmed at `aca9750`). Then restore the
test.

### Step A2: Rewrite `parseESDescriptors` to read the AudioSpecificConfig

Replace `parseESDescriptors` (`codecs.go:121-166`) with the three functions below. Keep the name and
signature `parseESDescriptors(data []byte) uint8`. The split into helpers is required: a single
function trips `gocyclo` (complexity 17 > 15). This shape was checked to lint clean.

```go
// MPEG-4 descriptor tags used inside an esds atom (ISO/IEC 14496-1 §7.2.2.1).
const (
	tagESDescriptor            = 0x03
	tagDecoderConfigDescriptor = 0x04
	tagDecoderSpecificInfo     = 0x05
)

// readDescriptorHeader reads an MPEG-4 descriptor tag at pos and its
// variable-length size (1-4 bytes, 7 bits each). It returns the payload
// size and the position of the payload, or ok=false if the tag differs or
// data runs out.
func readDescriptorHeader(data []byte, pos int, want byte) (size, payload int, ok bool) {
	if pos >= len(data) || data[pos] != want {
		return 0, 0, false
	}
	pos++
	for range 4 {
		if pos >= len(data) {
			return 0, 0, false
		}
		b := data[pos]
		pos++
		size = size<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	return size, pos, true
}

// audioObjectType decodes the leading audioObjectType of an
// AudioSpecificConfig: 5 bits, where 31 escapes to 32 + the next 6 bits.
func audioObjectType(asc []byte) uint8 {
	if len(asc) == 0 {
		return 0
	}
	aot := asc[0] >> 3
	if aot != 31 {
		return aot
	}
	if len(asc) < 2 {
		return 0
	}
	return 32 + ((asc[0]&0x07)<<3 | asc[1]>>5)
}

// parseESDescriptors walks ES_Descriptor → DecoderConfigDescriptor →
// DecoderSpecificInfo and returns the audioObjectType from the
// AudioSpecificConfig (ISO/IEC 14496-3 §1.6.2.1). Returns 0 when the
// structure is absent or truncated.
func parseESDescriptors(data []byte) uint8 {
	// ES_Descriptor payload: ES_ID(2), flags(1), then optional fields.
	_, pos, ok := readDescriptorHeader(data, 0, tagESDescriptor)
	if !ok || pos+3 > len(data) {
		return 0
	}
	flags := data[pos+2]
	pos += 3
	if flags&0x80 != 0 { // streamDependenceFlag set: skip the 2-byte dependsOn ES ID
		pos += 2
	}
	if flags&0x40 != 0 { // URL_Flag set: skip the length-prefixed URL
		if pos >= len(data) {
			return 0
		}
		pos += 1 + int(data[pos])
	}
	if flags&0x20 != 0 { // OCRstreamFlag set: skip the 2-byte OCR ES ID
		pos += 2
	}

	// DecoderConfigDescriptor payload starts with objectTypeIndication(1),
	// streamType(1), bufferSizeDB(3), maxBitrate(4), avgBitrate(4) = 13 bytes.
	_, pos, ok = readDescriptorHeader(data, pos, tagDecoderConfigDescriptor)
	if !ok {
		return 0
	}

	// DecoderSpecificInfo payload is the AudioSpecificConfig.
	size, pos, ok := readDescriptorHeader(data, pos+13, tagDecoderSpecificInfo)
	if !ok || size == 0 || pos >= len(data) {
		return 0
	}
	return audioObjectType(data[pos:min(pos+size, len(data))])
}
```

Notes:
- The old code scanned for a `0x03` byte anywhere. The new code requires the ES_Descriptor at
  offset 0, which is what the spec mandates: `esdsData` starts right after the esds version+flags.
- Keep the comment wording above. Comments shaped like `// name: field(2)` trip gocritic's
  `commentedOutCode` check.
- All indexing is guarded. Out-of-range positions fall through to `readDescriptorHeader`'s
  `pos >= len(data)` check.

**Verify**: `go test ./internal/m4a/ -run ESDescriptors -v` → all 6 subtests PASS (with the A1
codec test still temporarily removed, or after A3).

### Step A3: Bound the esds search to the sample entry

In `internal/m4a/technical.go:182`, keep the entry size instead of discarding it:

```go
	entrySize, err := binary.Read[uint32](sr, offset, "stsd entry size")
```

and at `:199` pass it through:

```go
	_ = parseCodecDetails(sr, offset-8, entrySize, codec, file)
```

Touch nothing else in `parseStsd`; plan 019 owns its offset arithmetic.

In `internal/m4a/codecs.go`, add a `sampleEntrySize uint32` parameter to `parseCodecDetails`, right
after `sampleEntryOffset`, and forward it to `parseAACProfile`. Then change the start of
`parseAACProfile`:

```go
// parseAACProfile attempts to extract AAC profile from ESDS atom.
// The search never reads past the sample entry, so a sample entry that sits
// near end-of-file is still examined.
func parseAACProfile(sr *binary.SafeReader, sampleEntryOffset int64, sampleEntrySize uint32) (string, error) {
	// Search for "esds" within the sample entry (first 256 bytes at most)
	searchLen := min(sampleEntrySize, 256)
	if searchLen < 12 {
		return "", nil
	}
	searchBuf := make([]byte, searchLen)
	if err := sr.ReadAt(searchBuf, sampleEntryOffset, "esds search buffer"); err != nil {
		return "", err
	}
```

After the existing `esdsSize < 12 || esdsSize > 1024` check, add a containment check:

```go
	if esdsOffset+int64(esdsSize) > sampleEntryOffset+int64(sampleEntrySize) {
		return "", nil // esds claims to extend past its sample entry
	}
```

Leave the rest of `parseAACProfile` (the `"esds"` scan, the 512-byte cap, the map lookup)
unchanged.

**Verify**: `go build ./...` → exit 0; `go test ./internal/m4a/ -run 'ESDescriptors|AACProfile|ParseTechnicalInfo' -v`
→ all PASS. If you removed `TestParseCodecDetails_AACProfile` in A1, it must be back. Then commit
Part A.

### Step B1: Failing tests for brand detection

Append to `format_test.go` (repo root, `package audiometa`; `bytes` and `encoding/binary` are already
imported there):

```go
// createFtyp builds an ftyp-only file with the given major and compatible brands.
func createFtyp(major string, compatible ...string) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, uint32(16+4*len(compatible)))
	buf.WriteString("ftyp")
	buf.WriteString(major)
	binary.Write(buf, binary.BigEndian, uint32(0))
	for _, b := range compatible {
		buf.WriteString(b)
	}
	return buf.Bytes()
}

func TestDetectFormat_Brands(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    Format
		wantErr bool
	}{
		{"M4B major", createFtyp("M4B "), FormatM4B, false},
		{"isom with compatible M4B", createFtyp("isom", "isom", "M4B "), FormatM4B, false},
		{"mp42 with compatible M4B", createFtyp("mp42", "M4B ", "mp42"), FormatM4B, false},
		{"mp41 major", createFtyp("mp41", "mp41", "isom"), FormatM4A, false},
		{"iso2 major", createFtyp("iso2", "iso2", "mp41"), FormatM4A, false},
		{"M4P major", createFtyp("M4P ", "M4P ", "mp42"), FormatM4A, false},
		{"unknown major, allowlisted compatible", createFtyp("dash", "iso6", "mp41"), FormatM4A, false},
		{"unknown brands only", createFtyp("XXXX", "YYYY"), FormatUnknown, true},
		{"truncated after ftyp header", createFtyp("M4A ")[:10], FormatUnknown, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectFormat(bytes.NewReader(tt.data), int64(len(tt.data)), "test.mp4")
			if (err != nil) != tt.wantErr {
				t.Fatalf("DetectFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("DetectFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

**Verify**: `go test . -run DetectFormat_Brands -v` → FAIL. The cases `isom/mp42 with compatible M4B`
give `M4A, want M4B`, and `mp41`/`iso2`/`M4P`/`dash` give `unsupported file brand`. This was
confirmed at `aca9750`.

### Step B2: Make `types.DetectFormat` read major + compatible brands

In `internal/types/format.go`, replace everything from `// Read major brand (next 4 bytes)` (`:174`)
to the end of `DetectFormat` (`:206`) with a single delegation. Then add the helper and allowlist
below `DetectFormat`, and add `"slices"` to the imports:

```go
	return detectMP4Brand(sr, int64(atomSize), size, path)
}

// maxFtypSize bounds how much of the ftyp atom is read for brand detection.
// Real ftyp atoms list a handful of brands; 1 KiB is generous.
const maxFtypSize = 1024

// mp4AudioBrands are the ISO-BMFF brands (major or compatible) accepted as
// M4A. "M4B " is handled separately because it selects FormatM4B.
var mp4AudioBrands = map[string]bool{
	"M4A ": true, // iTunes audio
	"M4P ": true, // iTunes protected (FairPlay) audio
	"mp41": true, // MP4 v1
	"mp42": true, // MP4 v2
	"isom": true, // ISO base media
	"iso2": true, // ISO base media, 2nd edition
}

// detectMP4Brand classifies an ftyp atom (starting at offset 0, ftypSize
// bytes long) by its major and compatible brands: any "M4B " brand selects
// FormatM4B; otherwise any brand in mp4AudioBrands selects FormatM4A.
func detectMP4Brand(sr *binary.SafeReader, ftypSize, fileSize int64, path string) (Format, error) {
	// ftyp payload: major brand(4), minor version(4), compatible brands(4 each).
	end := min(ftypSize, fileSize, maxFtypSize)
	if end < 12 {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}
	buf := make([]byte, end-8)
	if err := sr.ReadAt(buf, 8, "ftyp brands"); err != nil {
		return FormatUnknown, &UnsupportedFormatError{Path: path, Reason: "failed to read major brand"}
	}

	brands := []string{string(buf[0:4])}
	for i := 8; i+4 <= len(buf); i += 4 {
		brands = append(brands, string(buf[i:i+4]))
	}

	if slices.Contains(brands, "M4B ") {
		return FormatM4B, nil
	}
	if slices.ContainsFunc(brands, func(b string) bool { return mp4AudioBrands[b] }) {
		return FormatM4A, nil
	}

	return FormatUnknown, &UnsupportedFormatError{
		Path:   path,
		Reason: "unsupported file brand",
	}
}
```

The `end < 12` guard is load-bearing. Without it, a 9–11 byte file with a valid `ftyp` header panics
on `buf[0:4]`; the "truncated" test case covers this. Brands are 4 raw bytes with no trimming:
`"M4B "` includes the trailing space. A file with `M4B ` *anywhere* in its brands is an audiobook,
which matches the old `detectM4Format` semantics.

**Verify**: `go test . ./internal/types/ -run DetectFormat -v` → all PASS, including the
pre-existing `TestDetectFormat_UnsupportedBrand` and `_MP42`.

### Step B3: Make `m4a.detectM4Format` delegate to the single owner

In `internal/m4a/parser.go`, replace `detectM4Format` (`:15-54`) with:

```go
// detectM4Format classifies the file as M4A or M4B using the same ftyp brand
// rules as types.DetectFormat, so Parse and the public Open path agree.
// Files DetectFormat rejects (no ftyp, unknown brand) default to M4A.
func detectM4Format(r io.ReaderAt, size int64, path string) types.Format {
	if format, err := types.DetectFormat(r, size, path); err == nil && format == types.FormatM4B {
		return types.FormatM4B
	}
	return types.FormatM4A
}
```

and update the call at `:66` to `format := detectM4Format(r, size, path)`. The M4A fallback keeps
`TestParse_UnknownBrand` green, because `Parse` stays permissive when called directly.

**Verify**: `go test ./internal/m4a/ -run 'Parse_' -v` → all PASS;
`grep -n 'compatibleBrands' internal/m4a/parser.go` → no matches.

### Step B4: Full gates

**Verify**: `gofmt -l .` → no output; `go vet ./...` → exit 0; `go test -race ./...` → all `ok`;
`mise exec -- golangci-lint run ./...` → `0 issues.` (This was checked on a scratch copy at
`aca9750`: 0 issues, and the existing `//nolint:gocyclo` on `DetectFormat` is still accepted.)
Then commit Part B.

## Test plan

- Part A, `internal/m4a/esds_test.go` (new). Cover `parseESDescriptors` for AAC-LC (2), explicit
  HE-AAC (5), escaped AOT 42, streamDependenceFlag set, truncated, and empty input. Also cover an
  end-to-end `parseCodecDetails` on a real-shaped version-0 `mp4a` sample entry that is the whole
  file (fewer than 256 bytes to EOF), asserting `CodecProfile` and `CodecDescription`.
- Part B, `format_test.go` (append). Cover the brand table: compatible-M4B detection under
  `isom`/`mp42` major brands, newly accepted `mp41`/`iso2`/`M4P `, acceptance via a compatible
  brand, rejection of all-unknown brands, and a truncated ftyp that must not panic.
- Existing tests that must pass unchanged: `format_test.go` (`TestDetectFormat_*`),
  `internal/types/format_test.go`, `internal/m4a/parser_test.go` (`TestParse_Success`,
  `TestParse_UnknownBrand`), and `internal/m4a/codecs_test.go`.
- Patterns: `internal/m4a/atoms_test.go` (`createMockAtom`); `format_test.go:11` (`createMockM4B`).

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` empty
- [ ] `go test -race ./...` all `ok`, including `TestParseESDescriptors`, `TestParseCodecDetails_AACProfile`, `TestDetectFormat_Brands`
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'return data\[pos\]' internal/m4a/codecs.go` → no matches
- [ ] `grep -n 'make(\[\]byte, 256)' internal/m4a/codecs.go` → no matches
- [ ] `git status` shows only in-scope files changed
- [ ] `plans/README.md` status row for 030 added/updated

## STOP conditions

- Plan 019 part D has landed and `parseStsd` no longer reads the entry size at a single place, or no
  longer calls `parseCodecDetails(sr, offset-8, …)`. Report this and wire `entrySize` from wherever
  019 now reads the sample entry, but only if that is a one-line change. Otherwise STOP.
- Any pre-existing test asserts `CodecProfile == ""` or `CodecDescription == "AAC"` for a fixture
  that carries a real esds. That test encodes the bug; report it rather than editing it.
- `TestDetectFormat_UnsupportedBrand` starts passing a non-error result. That means the allowlist
  matched `XXXX`, i.e. something is wrong in Step B2.
- You feel the need to add brands beyond the six in `mp4AudioBrands` (e.g. `dash`, `iso6`, `M4V `,
  `qt  `). Do not; report the request. Broadening acceptance is a product decision, since those
  brands also label video.

## Maintenance notes

- **Behavior changes.** (1) `CodecProfile` is now populated for AAC (`AAC-LC`, `HE-AAC`,
  `HE-AAC v2`, `xHE-AAC`, …), and `CodecDescription` becomes the profile name for non-LC.
  Downstream `AudioInfo` helpers at `internal/types/audio.go:197,214` start firing for M4A.
  (2) Files with major brand `mp41`/`iso2`/`M4P `, or with any of those or `M4B ` as a compatible
  brand, now open instead of returning `UnsupportedFormatError`. (3) `isom`/`mp42` files that list
  `M4B ` as compatible now report `FormatM4B`.
- Implicit HE-AAC (SBR signalled only inside the AAC bitstream, AOT 2 in the ASC) is still reported
  as `AAC-LC`. Detecting the explicit backward-compatible form (sync extension `0x2b7` after the
  GASpecificConfig) is a reasonable follow-up, but it requires parsing GASpecificConfig and should
  be planned separately.
- `mp4AudioBrands` is the single allowlist. Extend it there, with a comment and a test row, if real
  files with other brands (e.g. `dash`, `iso5`/`iso6`) need to open. Note that `isom`/`mp42`
  already admit video MP4s; this plan does not change that.
- If plan 019 part D adds sample-entry v1/v2 handling, the esds search window (`min(entrySize, 256)`)
  still covers v2 entries (72-byte fixed part). There is nothing to revisit unless the 256 cap is
  removed.
- Reviewer: check the escape arithmetic in `audioObjectType`
  (`32 + ((asc[0]&0x07)<<3 | asc[1]>>5)`). The parentheses matter: Go's `<<` binds tighter than
  `|`, and `|` has the same precedence as `+`.
