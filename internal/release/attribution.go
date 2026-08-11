// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HK47196/zill/internal/gamefmt/paa"
	"github.com/HK47196/zill/internal/gamefmt/textureoverride"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	titleAttributionMemberIndex = 13587
	titleAttributionMemberName  = "2d/title/title1.gim"
	titleAttributionMemberSize  = 131792
	titleAttributionMemberHash  = "d30323fbc1b9da47cf8e570d6ecbdbb9e7a0a05b6234f1840e505ce990d77ee5"
	titleAttributionWidth       = 512
	titleAttributionHeight      = 256
	titleAttributionLeft        = 220
	titleAttributionRight       = 384
	titleAttributionTop         = 222
	titleAttributionBottom      = 245
)

type attributionConfig struct {
	Format  string `toml:"format"`
	Version int    `toml:"version"`
	Credit  string `toml:"credit"`
	URL     string `toml:"url"`
}

func parseAttributionConfig(data []byte) (attributionConfig, error) {
	var config attributionConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return attributionConfig{}, fmt.Errorf("decode title attribution: %w", err)
	}
	if config.Format != "zill-title-attribution" || config.Version != 1 {
		return attributionConfig{}, fmt.Errorf("unsupported title attribution format %q version %d", config.Format, config.Version)
	}
	if config.Credit == "" || config.URL == "" || strings.TrimSpace(config.Credit) != config.Credit || strings.TrimSpace(config.URL) != config.URL {
		return attributionConfig{}, fmt.Errorf("title attribution credit and URL must be nonempty and trimmed")
	}
	if strings.ContainsAny(config.Credit+config.URL, "\r\n") {
		return attributionConfig{}, fmt.Errorf("title attribution credit and URL must each use one line")
	}
	return config, nil
}

func loadAttributionConfig(root string) (attributionConfig, error) {
	data, err := read(root, "release", "title", "attribution.toml")
	if err != nil {
		return attributionConfig{}, err
	}
	return parseAttributionConfig(data)
}

func renderTitleAttribution(root string, config attributionConfig, version string) (*image.NRGBA, error) {
	if version == "" || strings.TrimSpace(version) != version || strings.ContainsAny(version, "\r\n") {
		return nil, fmt.Errorf("invalid title attribution version %q", version)
	}
	fontData, err := os.ReadFile(filepath.Join(root, "release", "font", "fs-tahoma-8px.otf"))
	if err != nil {
		return nil, fmt.Errorf("read title attribution font: %w", err)
	}
	parsed, err := opentype.Parse(fontData)
	if err != nil {
		return nil, fmt.Errorf("parse title attribution font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("create title attribution font face: %w", err)
	}
	if closer, ok := face.(io.Closer); ok {
		defer closer.Close()
	}

	lines := []string{config.Credit, version + " · " + config.URL}
	mask := image.NewAlpha(image.Rect(0, 0, titleAttributionWidth, titleAttributionHeight))
	drawer := font.Drawer{Dst: mask, Src: image.White, Face: face}
	for index, line := range lines {
		width := drawer.MeasureString(line).Ceil()
		checkedWidth := width
		if index == 1 && strings.HasSuffix(version, "-dirty") {
			checkedLine := strings.TrimSuffix(version, "-dirty") + " · " + config.URL
			checkedWidth = drawer.MeasureString(checkedLine).Ceil()
		}
		if checkedWidth > titleAttributionRight-titleAttributionLeft {
			return nil, fmt.Errorf("title attribution line %q is %d pixels wide; capacity is %d", line, width, titleAttributionRight-titleAttributionLeft)
		}
		drawer.Dot = fixed.P(titleAttributionRight-width, titleAttributionTop+9+index*12)
		drawer.DrawString(line)
	}

	overlay := image.NewNRGBA(mask.Bounds())
	available := [...]uint8{17, 34, 51, 68, 85, 119, 136, 153, 187, 204, 238, 255}
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			coverage := mask.AlphaAt(x, y).A
			if coverage == 0 {
				continue
			}
			value := available[0]
			bestDistance := int(coverage) - int(value)
			if bestDistance < 0 {
				bestDistance = -bestDistance
			}
			for _, candidate := range available[1:] {
				distance := int(coverage) - int(candidate)
				if distance < 0 {
					distance = -distance
				}
				if distance < bestDistance {
					value, bestDistance = candidate, distance
				}
			}
			overlay.SetNRGBA(x, y, color.NRGBA{value, value, value, 255})
		}
	}
	return overlay, nil
}

func addTitleAttribution(overlay image.Image, archives []*archive) error {
	var owner *archive
	for _, archive := range archives {
		if archive.name == "pa" {
			owner = archive
			break
		}
	}
	if owner == nil {
		return fmt.Errorf("retail archives do not contain pa")
	}
	pair := owner.pair
	members := pair.Members()
	if titleAttributionMemberIndex >= len(members) {
		return fmt.Errorf("pa archive is missing title attribution member %d", titleAttributionMemberIndex)
	}
	member := members[titleAttributionMemberIndex]
	if member.Name != titleAttributionMemberName || member.Size != titleAttributionMemberSize {
		return fmt.Errorf("pa member %d is %q size %d; want %q size %d", member.Index, member.Name, member.Size, titleAttributionMemberName, titleAttributionMemberSize)
	}
	source, err := pair.Payload(member.Index)
	if err != nil {
		return err
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(source)); actual != titleAttributionMemberHash {
		return fmt.Errorf("unsupported title attribution source fingerprint %s", actual)
	}
	edited, err := textureoverride.OverlayGIM(source, overlay)
	if err != nil {
		return fmt.Errorf("apply title attribution: %w", err)
	}
	owner.replacements = append(owner.replacements, paa.IndexReplacement(member.Index, edited))
	return nil
}
