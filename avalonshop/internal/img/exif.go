package img

import "encoding/binary"

// Orientation returns the EXIF orientation (1–8) of a JPEG, or 1 if the file
// is not a JPEG or carries no orientation tag. ponytail: 50-line APP1/TIFF
// walker instead of an EXIF library; we only ever need tag 0x0112.
func Orientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		switch {
		case marker == 0xD8, marker == 0x01, marker >= 0xD0 && marker <= 0xD7:
			i += 2 // standalone markers have no length
			continue
		case marker == 0xDA, marker == 0xD9:
			return 1 // start of scan / end: no APP1 ahead
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		if marker == 0xE1 {
			return tiffOrientation(b[i+4 : i+2+size])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 1
	}
	t := seg[6:]
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(t[2:4]) != 0x2A {
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off : off+2]))
	for j := 0; j < n; j++ {
		e := off + 2 + j*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
