// arfmt.go — GNU ar archive reader and writer for gaston.
//
// Supports the standard Unix ar format used by GNU binutils:
//
//	"!<arch>\n"                    — 8-byte magic
//	ar_hdr (60 bytes per member):
//	  ar_name[16]   space-padded name; "/" = symbol index; "name/" = regular member
//	  ar_date[12]   mtime as decimal ASCII
//	  ar_uid[6]     owner UID
//	  ar_gid[6]     owner GID
//	  ar_mode[8]    octal file mode
//	  ar_size[10]   member size in decimal ASCII
//	  ar_fmag[2]    "`\n"
//	member data     padded to even byte boundary
//
// The first member (name "/") is the symbol index.  Its data is:
//	4-byte big-endian symbol count N
//	N × 4-byte big-endian file offsets (to the ar_hdr of the defining member)
//	N null-terminated symbol names (concatenated)
//
// Usage:
//
//	gaston -ar -o libfoo.a a.o b.o …   — build archive
//	gaston -link -o prog main.o libfoo.a — link with lazy extraction
package main

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const arFileMagic = "!<arch>\n"
const arHdrLen    = 60
const arEndMagic  = "`\n"

// arMember is one member extracted from an ar archive.
type arMember struct {
	name string // original filename (without trailing "/")
	data []byte // raw content (the .o file bytes)
}

// arPadRight returns a space-padded []byte slice of length n.
func arPadRight(s string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	copy(b, s)
	return b
}

// archiveCreate builds a GNU ar archive at outpath from the given .o files.
// A symbol index member ("/") is written first so the linker can lazy-link.
func archiveCreate(outpath string, objpaths []string) error {
	// Load all members.
	members := make([]arMember, len(objpaths))
	for i, p := range objpaths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("archive: read %s: %w", p, err)
		}
		members[i] = arMember{name: filepath.Base(p), data: data}
	}

	// Extract global defined symbols from each member for the index.
	type symEntry struct {
		sym       string
		memberIdx int
	}
	var allSyms []symEntry
	for i, m := range members {
		obj, err := loadObjFromBytes(m.name, m.data)
		if err != nil {
			// Still include the member; just omit it from the index.
			continue
		}
		for _, sym := range obj.syms {
			if sym.binding == elf.STB_GLOBAL && sym.secName != "" && sym.name != "" {
				allSyms = append(allSyms, symEntry{sym.name, i})
			}
		}
	}

	// Build the symbol index payload:
	//   4 bytes BE count, N×4 bytes BE member offsets, null-terminated names.
	var namesBlob []byte
	for _, s := range allSyms {
		namesBlob = append(namesBlob, []byte(s.sym)...)
		namesBlob = append(namesBlob, 0)
	}
	symIdxSize := 4 + len(allSyms)*4 + len(namesBlob)
	symIdxPadded := symIdxSize
	if symIdxPadded%2 != 0 {
		symIdxPadded++
	}

	// Compute byte offset of each regular member's ar_hdr within the archive:
	//   8 (magic) + arHdrLen (symidx hdr) + symIdxPadded + sum of previous members.
	memberFileOff := make([]uint32, len(members))
	off := uint32(8 + arHdrLen + symIdxPadded)
	for i, m := range members {
		memberFileOff[i] = off
		dataPadded := uint32(len(m.data))
		if dataPadded%2 != 0 {
			dataPadded++
		}
		off += uint32(arHdrLen) + dataPadded
	}

	// Build symbol index data.
	symIdxData := make([]byte, symIdxSize)
	binary.BigEndian.PutUint32(symIdxData[0:4], uint32(len(allSyms)))
	for i, s := range allSyms {
		binary.BigEndian.PutUint32(symIdxData[4+i*4:], memberFileOff[s.memberIdx])
	}
	copy(symIdxData[4+len(allSyms)*4:], namesBlob)

	// Write the archive.
	f, err := os.OpenFile(outpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("archive: create %s: %w", outpath, err)
	}
	defer f.Close()

	writeHdr := func(name string, size int) {
		var hdr [arHdrLen]byte
		copy(hdr[0:16], arPadRight(name, 16))
		copy(hdr[16:28], arPadRight("0", 12))   // mtime
		copy(hdr[28:34], arPadRight("0", 6))    // uid
		copy(hdr[34:40], arPadRight("0", 6))    // gid
		copy(hdr[40:48], arPadRight("644", 8))  // mode
		copy(hdr[48:58], arPadRight(strconv.Itoa(size), 10))
		copy(hdr[58:60], arEndMagic)
		f.Write(hdr[:])
	}

	// Magic header.
	f.Write([]byte(arFileMagic))

	// Symbol index member.
	writeHdr("/", symIdxSize)
	f.Write(symIdxData)
	if symIdxSize%2 != 0 {
		f.Write([]byte{0})
	}

	// Regular members.
	for _, m := range members {
		writeHdr(m.name+"/", len(m.data))
		f.Write(m.data)
		if len(m.data)%2 != 0 {
			f.Write([]byte{0})
		}
	}

	return nil
}

// archiveRead parses a GNU ar archive and returns its members together with
// a symbol-name → member-index map (built by scanning each member's symbol
// table) for use by the lazy linker.
func archiveRead(path string) (members []arMember, symMap map[string]int, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, nil, fmt.Errorf("archive: read %s: %w", path, readErr)
	}
	if len(data) < 8 || string(data[:8]) != arFileMagic {
		return nil, nil, fmt.Errorf("archive: %s: not an ar archive", path)
	}

	symMap = make(map[string]int)
	off := 8

	for off+arHdrLen <= len(data) {
		// Parse ar_hdr.
		nameField := strings.TrimRight(string(data[off:off+16]), " ")
		sizeStr   := strings.TrimRight(string(data[off+48:off+58]), " ")
		size, _   := strconv.Atoi(sizeStr)
		if size < 0 || off+arHdrLen+size > len(data) {
			return nil, nil, fmt.Errorf("archive: %s: truncated member at offset %d", path, off)
		}

		memberData := data[off+arHdrLen : off+arHdrLen+size]
		off += arHdrLen + size
		if size%2 != 0 {
			off++ // padding byte
		}

		switch nameField {
		case "/":
			// Symbol index — skip (we rebuild from member scans below).
			continue
		case "//":
			// Long filename table — not used by our archives.
			continue
		}

		// Strip trailing "/" from GNU-style member names.
		cleanName := strings.TrimSuffix(nameField, "/")
		members = append(members, arMember{name: cleanName, data: memberData})
	}

	// Build symMap by scanning each member's symbol table.
	for i, m := range members {
		obj, parseErr := loadObjFromBytes(m.name, m.data)
		if parseErr != nil {
			continue
		}
		for _, sym := range obj.syms {
			if sym.binding == elf.STB_GLOBAL && sym.secName != "" && sym.name != "" {
				if _, already := symMap[sym.name]; !already {
					symMap[sym.name] = i
				}
			}
		}
	}

	return members, symMap, nil
}
