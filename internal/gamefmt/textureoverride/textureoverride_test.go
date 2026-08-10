package textureoverride_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/HK47196/zill/internal/gamefmt/paa"
	"github.com/HK47196/zill/internal/gamefmt/textureoverride"
)

func TestCompileChangesSelectedPixelsAndPreservesContainerBytes(t *testing.T) {
	gim := fixtureGIM([]byte{0x10})
	sibling := []byte("untouched sibling bytes")
	member := fixturePAR([]namedPayload{{"target.gim", gim}, {"sibling.bin", sibling}})
	pair := fixturePair(t, "2d/icon/icons.par", member)
	defer pair.Close()

	root := t.TempDir()
	overridePath := filepath.Join(root, "pa", "000000", "2d", "icon", "icons.par", "000000_target.gim.png")
	writePNG(t, overridePath, []color.NRGBA{{255, 255, 255, 255}, {0, 0, 0, 255}})
	replacements, err := textureoverride.Compile(root, map[string]*paa.Pair{"pa": pair})
	if err != nil {
		t.Fatal(err)
	}
	if len(replacements["pa"]) != 1 || replacements["pa"][0].Index == nil || *replacements["pa"][0].Index != 0 {
		t.Fatalf("replacements = %#v, want one replacement for pa member 0", replacements)
	}
	got := replacements["pa"][0].Payload
	if len(got) != len(member) {
		t.Fatalf("replacement length = %d, want %d", len(got), len(member))
	}
	differences := differingOffsets(member, got)
	if len(differences) != 1 {
		t.Fatalf("changed offsets = %v, want exactly the selected packed-pixel byte", differences)
	}
	if member[differences[0]] != 0x10 || got[differences[0]] != 0x01 {
		t.Fatalf("changed byte = %#x -> %#x, want packed indices %#x -> %#x", member[differences[0]], got[differences[0]], 0x10, 0x01)
	}
	children := parChildren(t, got)
	if !bytes.Equal(got[children[1][0]:children[1][1]], sibling) {
		t.Fatal("untouched PAR sibling changed")
	}
}

func TestCompileRejectsColorAbsentFromPaletteWithoutReturningReplacement(t *testing.T) {
	member := fixturePAR([]namedPayload{{"target.gim", fixtureGIM([]byte{0x10})}})
	pair := fixturePair(t, "icons.par", member)
	defer pair.Close()
	root := t.TempDir()
	path := filepath.Join(root, "pa", "000000", "icons.par", "000000_target.gim.png")
	writePNG(t, path, []color.NRGBA{{255, 0, 0, 255}, {0, 0, 0, 255}})

	replacements, err := textureoverride.Compile(root, map[string]*paa.Pair{"pa": pair})
	if err == nil {
		t.Fatal("Compile accepted a PNG color absent from the source palette")
	}
	if replacements != nil {
		t.Fatalf("failed Compile returned replacements: %#v", replacements)
	}
	source, readErr := pair.Payload(0)
	if readErr != nil || !bytes.Equal(source, member) {
		t.Fatal("failed Compile mutated the source PAA member")
	}
}

func TestCompileRejectsMismatchedPathLocators(t *testing.T) {
	member := fixturePAR([]namedPayload{{"target.gim", fixtureGIM([]byte{0x10})}})
	for _, test := range []struct {
		name string
		path string
	}{
		{"member name", "pa/000000/wrong.par/000000_target.gim.png"},
		{"member index width", "pa/0/icons.par/000000_target.gim.png"},
		{"PAR child index", "pa/000000/icons.par/000001_target.gim.png"},
		{"PAR child name", "pa/000000/icons.par/000000_wrong.gim.png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pair := fixturePair(t, "icons.par", member)
			defer pair.Close()
			root := t.TempDir()
			writePNG(t, filepath.Join(root, filepath.FromSlash(test.path)), []color.NRGBA{{0, 0, 0, 255}, {255, 255, 255, 255}})
			got, err := textureoverride.Compile(root, map[string]*paa.Pair{"pa": pair})
			if err == nil {
				t.Fatal("Compile accepted a mismatched texture locator")
			}
			if got != nil {
				t.Fatalf("failed Compile returned replacements: %#v", got)
			}
		})
	}
}

func writePNG(t *testing.T, path string, pixels []color.NRGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	for x, value := range pixels {
		img.SetNRGBA(x, 0, value)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

type namedPayload struct {
	name    string
	payload []byte
}

func fixturePAR(children []namedPayload) []byte {
	headerEnd := align16(16+len(children)*4) + len(children)*32
	result := make([]byte, headerEnd)
	copy(result, []byte{'P', 'A', 'R', 0})
	binary.LittleEndian.PutUint32(result[8:], uint32(len(children)))
	for i, child := range children {
		start := align16(len(result))
		result = append(result, make([]byte, start-len(result))...)
		binary.LittleEndian.PutUint32(result[16+i*4:], uint32(start))
		copy(result[align16(16+len(children)*4)+i*32:], child.name)
		result = append(result, child.payload...)
	}
	return result
}

func fixtureGIM(indices []byte) []byte {
	imageBlock := fixtureSurface(4, 2, 1, 4, 1, 1, indices)
	palette := []byte{0x00, 0xf0, 0xff, 0xff}
	paletteBlock := fixtureSurface(2, 2, 1, 16, 2, 1, palette)
	binary.LittleEndian.PutUint32(imageBlock[8:], uint32(len(imageBlock)))
	picture := make([]byte, 16, 16+len(imageBlock)+len(paletteBlock))
	binary.LittleEndian.PutUint32(picture[0:], 3)
	binary.LittleEndian.PutUint32(picture[4:], uint32(cap(picture)))
	binary.LittleEndian.PutUint32(picture[12:], 16)
	picture = append(picture, imageBlock...)
	picture = append(picture, paletteBlock...)
	root := make([]byte, 16, 16+len(picture))
	binary.LittleEndian.PutUint32(root[0:], 2)
	binary.LittleEndian.PutUint32(root[4:], uint32(cap(root)))
	binary.LittleEndian.PutUint32(root[12:], 16)
	root = append(root, picture...)
	result := append([]byte("MIG.00.1PSP\x00\x00\x00\x00\x00"), root...)
	return result
}

func fixtureSurface(format, width, height, bits, pitchAlign, heightAlign int, pixels []byte) []byte {
	const descriptorOffset = 16
	const pixelOffset = 0x34
	block := make([]byte, descriptorOffset+pixelOffset, descriptorOffset+pixelOffset+len(pixels))
	binary.LittleEndian.PutUint32(block[0:], 4)
	if format == 2 {
		binary.LittleEndian.PutUint32(block[0:], 5)
	}
	binary.LittleEndian.PutUint32(block[4:], uint32(cap(block)))
	binary.LittleEndian.PutUint32(block[12:], descriptorOffset)
	d := block[descriptorOffset:]
	binary.LittleEndian.PutUint32(d[0:], 0x30)
	binary.LittleEndian.PutUint16(d[4:], uint16(format))
	binary.LittleEndian.PutUint16(d[6:], 0)
	binary.LittleEndian.PutUint16(d[8:], uint16(width))
	binary.LittleEndian.PutUint16(d[10:], uint16(height))
	binary.LittleEndian.PutUint16(d[12:], uint16(bits))
	binary.LittleEndian.PutUint16(d[14:], uint16(pitchAlign))
	binary.LittleEndian.PutUint16(d[16:], uint16(heightAlign))
	binary.LittleEndian.PutUint32(d[24:], 0x30)
	binary.LittleEndian.PutUint32(d[28:], pixelOffset)
	binary.LittleEndian.PutUint32(d[32:], uint32(pixelOffset+len(pixels)))
	levelType := uint16(1)
	if format == 2 {
		levelType = 2
	}
	binary.LittleEndian.PutUint16(d[40:], levelType)
	binary.LittleEndian.PutUint16(d[42:], 1)
	binary.LittleEndian.PutUint16(d[44:], 3)
	binary.LittleEndian.PutUint16(d[46:], 1)
	binary.LittleEndian.PutUint32(d[48:], pixelOffset)
	return append(block, pixels...)
}

func fixturePair(t *testing.T, name string, payload []byte) *paa.Pair {
	t.Helper()
	dir := t.TempDir()
	index := bytes.Repeat([]byte{0xcc}, 0x64)
	copy(index, []byte{'P', 'A', 'A', 0})
	binary.LittleEndian.PutUint32(index[8:], 1)
	binary.LittleEndian.PutUint32(index[16:], 0x60)
	binary.LittleEndian.PutUint32(index[0x20:], 0x40)
	binary.LittleEndian.PutUint32(index[0x24:], uint32(len(payload)))
	for i := 0x40; i < 0x60; i++ {
		index[i] = 0
	}
	copy(index[0x40:], name)
	binary.LittleEndian.PutUint32(index[0x60:], 0x10)
	archive := make([]byte, 16, align16(16+len(payload)))
	archive = append(archive, payload...)
	archive = append(archive, make([]byte, cap(archive)-len(archive))...)
	indexPath := filepath.Join(dir, "pa.bin")
	archivePath := filepath.Join(dir, "pa.arc")
	if err := os.WriteFile(indexPath, index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	pair, err := paa.Open(indexPath, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func parChildren(t *testing.T, data []byte) [][2]int {
	t.Helper()
	count := int(binary.LittleEndian.Uint32(data[8:]))
	result := make([][2]int, count)
	for i := range result {
		result[i][0] = int(binary.LittleEndian.Uint32(data[16+i*4:]))
		result[i][1] = len(data)
		if i+1 < count {
			result[i][1] = int(binary.LittleEndian.Uint32(data[16+(i+1)*4:]))
		}
	}
	return result
}

func differingOffsets(a, b []byte) []int {
	var result []int
	for i := range a {
		if a[i] != b[i] {
			result = append(result, i)
		}
	}
	return result
}

func align16(value int) int { return (value + 15) &^ 15 }
