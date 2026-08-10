// Package textureoverride compiles editable PNG texture overrides into PAA
// member replacements while preserving the source containers byte-for-byte
// outside the selected GIM's logical pixels.
package textureoverride

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/HK47196/zill/internal/gamefmt/paa"
)

const gimMagic = "MIG.00.1PSP\x00\x00\x00\x00\x00"

// Compile walks root for PNG overrides and returns replacements grouped by
// PAA archive name. It returns no replacements if any override is invalid.
func Compile(root string, archives map[string]*paa.Pair) (map[string][]paa.Replacement, error) {
	type target struct {
		archive string
		index   int
		steps   []step
		path    string
	}
	var targets []target
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		archive, index, steps, err := parseLocator(filepath.ToSlash(rel), archives)
		if err != nil {
			return err
		}
		targets = append(targets, target{archive, index, steps, path})
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return map[string][]paa.Replacement{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("texture overrides: %w", err)
	}

	type memberKey struct {
		archive string
		index   int
	}
	payloads := make(map[memberKey][]byte)
	for _, target := range targets {
		key := memberKey{target.archive, target.index}
		payload := payloads[key]
		if payload == nil {
			var err error
			payload, err = archives[target.archive].Payload(target.index)
			if err != nil {
				return nil, fmt.Errorf("%s: read source member: %w", target.path, err)
			}
		}
		pngData, err := os.ReadFile(target.path)
		if err != nil {
			return nil, fmt.Errorf("%s: read override: %w", target.path, err)
		}
		if err := apply(payload, target.steps, pngData, target.path); err != nil {
			return nil, err
		}
		payloads[key] = payload
	}

	result := make(map[string][]paa.Replacement)
	keys := make([]memberKey, 0, len(payloads))
	for key := range payloads {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].archive != keys[j].archive {
			return keys[i].archive < keys[j].archive
		}
		return keys[i].index < keys[j].index
	})
	for _, key := range keys {
		payload := payloads[key]
		result[key.archive] = append(result[key.archive], paa.IndexReplacement(key.index, payload))
	}
	return result, nil
}

type step struct {
	index int
	name  string
}

func parseLocator(path string, archives map[string]*paa.Pair) (string, int, []step, error) {
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] == "" {
		return "", 0, nil, fmt.Errorf("%s: expected ARCHIVE/six-digit-index/member/child.png", path)
	}
	pair := archives[parts[0]]
	if pair == nil {
		return "", 0, nil, fmt.Errorf("%s: unknown PAA archive %q", path, parts[0])
	}
	if len(parts[1]) != 6 {
		return "", 0, nil, fmt.Errorf("%s: PAA member index must use six digits", path)
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 {
		return "", 0, nil, fmt.Errorf("%s: invalid PAA member index", path)
	}
	members := pair.Members()
	if index >= len(members) {
		return "", 0, nil, fmt.Errorf("%s: PAA member index is out of range", path)
	}
	memberParts := strings.Split(filepath.ToSlash(members[index].Name), "/")
	if len(parts) <= 2+len(memberParts) || !equalStrings(parts[2:2+len(memberParts)], memberParts) {
		return "", 0, nil, fmt.Errorf("%s: PAA member name does not match index %06d (%q)", path, index, members[index].Name)
	}
	locatorParts := parts[2+len(memberParts):]
	steps := make([]step, len(locatorParts))
	for i, component := range locatorParts {
		if i == len(locatorParts)-1 {
			if filepath.Ext(component) != ".png" {
				return "", 0, nil, fmt.Errorf("%s: final locator must be a PNG", path)
			}
			component = strings.TrimSuffix(component, ".png")
		}
		parsed, err := parseStep(component)
		if err != nil {
			return "", 0, nil, fmt.Errorf("%s: %w", path, err)
		}
		steps[i] = parsed
	}
	return parts[0], index, steps, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func parseStep(component string) (step, error) {
	if len(component) < 8 || component[6] != '_' {
		return step{}, fmt.Errorf("PAR child locator %q must use six-digit-index_name", component)
	}
	index, err := strconv.Atoi(component[:6])
	if err != nil || index < 0 || component[7:] == "" {
		return step{}, fmt.Errorf("invalid PAR child locator %q", component)
	}
	return step{index, component[7:]}, nil
}

func apply(payload []byte, steps []step, pngData []byte, source string) error {
	current := payload
	for _, wanted := range steps {
		children, err := parsePAR(current)
		if err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		if wanted.index >= len(children) || children[wanted.index].name != wanted.name {
			return fmt.Errorf("%s: PAR child %06d_%s does not match source", source, wanted.index, wanted.name)
		}
		child := children[wanted.index]
		current = current[child.start:child.end]
	}
	if err := replaceGIMPixels(current, pngData); err != nil {
		return fmt.Errorf("%s: %w", source, err)
	}
	return nil
}

type parChild struct {
	name       string
	start, end int
}

func parsePAR(data []byte) ([]parChild, error) {
	if len(data) < 16 || !bytes.Equal(data[:4], []byte{'P', 'A', 'R', 0}) {
		return nil, errors.New("locator expects a PAR container")
	}
	count := int(binary.LittleEndian.Uint32(data[8:12]))
	if count > (len(data)-16)/4 {
		return nil, errors.New("truncated PAR offset table")
	}
	namesBase := align16(16 + count*4)
	if count > (len(data)-namesBase)/32 {
		return nil, errors.New("truncated PAR name table")
	}
	children := make([]parChild, count)
	previous := namesBase + count*32
	for i := 0; i < count; i++ {
		start := int(binary.LittleEndian.Uint32(data[16+i*4:]))
		if start < previous || start >= len(data) {
			return nil, fmt.Errorf("PAR child %d has an invalid offset", i)
		}
		end := len(data)
		if i+1 < count {
			end = int(binary.LittleEndian.Uint32(data[16+(i+1)*4:]))
			if end <= start || end > len(data) {
				return nil, fmt.Errorf("PAR child %d has an invalid end offset", i)
			}
		}
		raw := data[namesBase+i*32 : namesBase+(i+1)*32]
		if nul := bytes.IndexByte(raw, 0); nul >= 0 {
			raw = raw[:nul]
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("PAR child %d has an empty name", i)
		}
		for _, b := range raw {
			if b < 0x20 || b > 0x7e || b == '/' || b == '\\' {
				return nil, fmt.Errorf("PAR child %d has an unsupported name", i)
			}
		}
		children[i] = parChild{string(raw), start, end}
		previous = end
	}
	return children, nil
}

func align16(value int) int { return (value + 15) &^ 15 }

type surface struct {
	format, order, width, height, bits, pitch, storageHeight int
	pixelsStart, pixelsEnd                                   int
}

func replaceGIMPixels(data, pngData []byte) error {
	imageSurface, paletteSurface, err := parseGIM(data)
	if err != nil {
		return err
	}
	edited, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("decode edited PNG: %w", err)
	}
	if edited.Bounds().Dx() != imageSurface.width || edited.Bounds().Dy() != imageSurface.height {
		return fmt.Errorf("edited PNG is %dx%d, expected %dx%d", edited.Bounds().Dx(), edited.Bounds().Dy(), imageSurface.width, imageSurface.height)
	}
	palette := decodePalette(data, paletteSurface)
	lookup := make(map[color.NRGBA]int, len(palette))
	for i, value := range palette {
		if _, exists := lookup[value]; !exists {
			lookup[value] = i
		}
	}
	bounds := edited.Bounds()
	for y := 0; y < imageSurface.height; y++ {
		for x := 0; x < imageSurface.width; x++ {
			wanted := color.NRGBAModel.Convert(edited.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			index, ok := lookup[wanted]
			if !ok {
				return fmt.Errorf("edited PNG uses color %v absent from palette", wanted)
			}
			original := readIndex(data, imageSurface, x, y)
			if original >= len(palette) {
				return fmt.Errorf("pixel references missing palette index %d", original)
			}
			if palette[original] == wanted {
				index = original
			}
			writeIndex(data, imageSurface, x, y, index)
		}
	}
	return nil
}

func parseGIM(data []byte) (surface, surface, error) {
	if len(data) < 32 || string(data[:16]) != gimMagic {
		return surface{}, surface{}, errors.New("selected PAR child is not a little-endian PSP GIM")
	}
	rootEnd, err := blockEnd(data, 16, 2, len(data))
	if err != nil || rootEnd != 16+int(binary.LittleEndian.Uint32(data[20:24])) {
		return surface{}, surface{}, errors.New("invalid GIM root block")
	}
	pictureStart := 16 + int(binary.LittleEndian.Uint32(data[28:32]))
	pictureEnd, err := blockEnd(data, pictureStart, 3, rootEnd)
	if err != nil || pictureEnd != rootEnd {
		return surface{}, surface{}, errors.New("unsupported GIM picture layout (exactly one picture is required)")
	}
	imageStart := pictureStart + int(binary.LittleEndian.Uint32(data[pictureStart+12:pictureStart+16]))
	imageEnd, err := blockEnd(data, imageStart, 4, pictureEnd)
	if err != nil {
		return surface{}, surface{}, fmt.Errorf("invalid GIM image block: %w", err)
	}
	img, err := parseSurface(data, imageStart, imageEnd, false)
	if err != nil {
		return surface{}, surface{}, err
	}
	paletteStart := imageStart + int(binary.LittleEndian.Uint32(data[imageStart+8:imageStart+12]))
	if paletteStart != imageEnd {
		return surface{}, surface{}, errors.New("invalid GIM palette location")
	}
	paletteEnd, err := blockEnd(data, paletteStart, 5, pictureEnd)
	if err != nil || paletteEnd != pictureEnd {
		return surface{}, surface{}, errors.New("invalid GIM palette block")
	}
	pal, err := parseSurface(data, paletteStart, paletteEnd, true)
	if err != nil {
		return surface{}, surface{}, err
	}
	if pal.width*pal.height > 1<<img.bits {
		return surface{}, surface{}, errors.New("GIM palette has too many entries")
	}
	return img, pal, nil
}

func blockEnd(data []byte, start, kind, limit int) (int, error) {
	if start < 0 || start+16 > limit || start+16 > len(data) {
		return 0, errors.New("truncated block")
	}
	if int(binary.LittleEndian.Uint32(data[start:start+4])) != kind {
		return 0, errors.New("unexpected block type")
	}
	size := int(binary.LittleEndian.Uint32(data[start+4 : start+8]))
	if size < 16 || start+size < start || start+size > limit {
		return 0, errors.New("block extends past container")
	}
	return start + size, nil
}

func parseSurface(data []byte, start, end int, palette bool) (surface, error) {
	descriptor := start + int(binary.LittleEndian.Uint32(data[start+12:start+16]))
	if descriptor < start+16 || descriptor+52 > end {
		return surface{}, errors.New("truncated GIM surface descriptor")
	}
	format := int(binary.LittleEndian.Uint16(data[descriptor+4:]))
	order := int(binary.LittleEndian.Uint16(data[descriptor+6:]))
	width := int(binary.LittleEndian.Uint16(data[descriptor+8:]))
	height := int(binary.LittleEndian.Uint16(data[descriptor+10:]))
	bits := int(binary.LittleEndian.Uint16(data[descriptor+12:]))
	pitchAlign := int(binary.LittleEndian.Uint16(data[descriptor+14:]))
	heightAlign := int(binary.LittleEndian.Uint16(data[descriptor+16:]))
	if width == 0 || height == 0 || pitchAlign == 0 || heightAlign == 0 || order < 0 || order > 1 {
		return surface{}, errors.New("invalid GIM surface dimensions, alignment, or order")
	}
	if (!palette && !((format == 4 && bits == 4) || (format == 5 && bits == 8))) || (palette && !((format == 1 || format == 2) && bits == 16)) {
		return surface{}, fmt.Errorf("unsupported GIM surface format %d/%dbpp", format, bits)
	}
	descriptorSize := int(binary.LittleEndian.Uint32(data[descriptor:]))
	indexStart := int(binary.LittleEndian.Uint32(data[descriptor+24:]))
	pixelsRel := int(binary.LittleEndian.Uint32(data[descriptor+28:]))
	pixelsEndRel := int(binary.LittleEndian.Uint32(data[descriptor+32:]))
	levelType := binary.LittleEndian.Uint16(data[descriptor+40:])
	levelCount := binary.LittleEndian.Uint16(data[descriptor+42:])
	frameType := binary.LittleEndian.Uint16(data[descriptor+44:])
	frameCount := binary.LittleEndian.Uint16(data[descriptor+46:])
	wantLevel := uint16(1)
	if palette {
		wantLevel = 2
	}
	if descriptorSize < 48 || indexStart < descriptorSize || descriptor+indexStart+4 > end || binary.LittleEndian.Uint32(data[descriptor+indexStart:]) != uint32(pixelsRel) || levelType != wantLevel || levelCount != 1 || frameType != 3 || frameCount != 1 {
		return surface{}, errors.New("unsupported GIM mipmap or frame layout")
	}
	rowBytes := (width*bits + 7) / 8
	pitch := align(rowBytes, pitchAlign)
	storageHeight := align(height, heightAlign)
	if pixelsEndRel != pixelsRel+pitch*storageHeight || descriptor+pixelsRel < descriptor || descriptor+pixelsEndRel > end {
		return surface{}, errors.New("invalid GIM pixel payload bounds")
	}
	if order == 1 && (pitch%16 != 0 || storageHeight%8 != 0) {
		return surface{}, errors.New("invalid PSP-swizzled GIM alignment")
	}
	return surface{format, order, width, height, bits, pitch, storageHeight, descriptor + pixelsRel, descriptor + pixelsEndRel}, nil
}

func align(value, alignment int) int { return (value + alignment - 1) / alignment * alignment }

func storageOffset(s surface, xByte, y int) int {
	if s.order == 0 {
		return s.pixelsStart + y*s.pitch + xByte
	}
	return s.pixelsStart + ((y/8)*(s.pitch/16)+xByte/16)*128 + (y%8)*16 + xByte%16
}

func readIndex(data []byte, s surface, x, y int) int {
	if s.bits == 4 {
		value := data[storageOffset(s, x/2, y)]
		return int(value >> (4 * (x & 1)) & 15)
	}
	return int(data[storageOffset(s, x, y)])
}

func writeIndex(data []byte, s surface, x, y, index int) {
	if s.bits == 4 {
		offset := storageOffset(s, x/2, y)
		shift := 4 * (x & 1)
		mask := byte(15 << shift)
		data[offset] = data[offset]&^mask | byte(index<<shift)
		return
	}
	data[storageOffset(s, x, y)] = byte(index)
}

func decodePalette(data []byte, s surface) []color.NRGBA {
	result := make([]color.NRGBA, s.width*s.height)
	for i := range result {
		offset := storageOffset(s, (i%s.width)*2, i/s.width)
		value := binary.LittleEndian.Uint16(data[offset : offset+2])
		if s.format == 1 {
			result[i] = color.NRGBA{expand(value&31, 5), expand(value>>5&31, 5), expand(value>>10&31, 5), 0}
			if value&0x8000 != 0 {
				result[i].A = 255
			}
		} else {
			result[i] = color.NRGBA{uint8(value&15) * 17, uint8(value>>4&15) * 17, uint8(value>>8&15) * 17, uint8(value>>12&15) * 17}
		}
	}
	return result
}

func expand(value uint16, bits uint) uint8 {
	return uint8(value<<(8-bits) | value>>(2*bits-8))
}
