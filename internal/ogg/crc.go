package ogg

// crcPoly is the generator polynomial of the Ogg page checksum (RFC 3533 §6,
// field 7).
const crcPoly = 0x04C11DB7

// crcTable drives [crcUpdate]. Go's hash/crc32 only implements reflected
// CRCs, and the Ogg checksum is not reflected: it is computed most
// significant bit first, starts at 0 and has no final XOR.
var crcTable = func() (t [256]uint32) {
	for i := range t {
		r := uint32(i) << 24
		for range 8 {
			if r&0x80000000 != 0 {
				r = r<<1 ^ crcPoly
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

// crcUpdate returns crc advanced over b. The checksum of a page is
// crcUpdate(0, page) with the page's four CRC bytes zeroed.
func crcUpdate(crc uint32, b []byte) uint32 {
	for _, c := range b {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^c]
	}
	return crc
}
