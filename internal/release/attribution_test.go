// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"image/color"
	"strings"
	"testing"
)

func TestRenderTitleAttributionFitsBottomRightTitleCanvasAndUsesPaletteColors(t *testing.T) {
	config, err := loadAttributionConfig("../..")
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := renderTitleAttribution("../..", config, "v1.0-alpha")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[color.NRGBA]bool{
		{17, 17, 17, 255}:    true,
		{34, 34, 34, 255}:    true,
		{51, 51, 51, 255}:    true,
		{68, 68, 68, 255}:    true,
		{85, 85, 85, 255}:    true,
		{119, 119, 119, 255}: true,
		{136, 136, 136, 255}: true,
		{153, 153, 153, 255}: true,
		{187, 187, 187, 255}: true,
		{204, 204, 204, 255}: true,
		{238, 238, 238, 255}: true,
		{255, 255, 255, 255}: true,
	}
	visible := 0
	visibleByLine := [2]int{}
	for y := overlay.Bounds().Min.Y; y < overlay.Bounds().Max.Y; y++ {
		for x := overlay.Bounds().Min.X; x < overlay.Bounds().Max.X; x++ {
			pixel := overlay.NRGBAAt(x, y)
			if pixel.A == 0 {
				continue
			}
			visible++
			line := 0
			if y >= titleAttributionTop+12 {
				line = 1
			}
			visibleByLine[line]++
			if x < titleAttributionLeft || x >= titleAttributionRight || y < titleAttributionTop || y >= titleAttributionBottom {
				t.Fatalf("attribution pixel (%d,%d) is outside the bottom-right title canvas", x, y)
			}
			if !allowed[pixel] {
				t.Fatalf("attribution pixel uses unsupported palette color %v", pixel)
			}
		}
	}
	if visible == 0 {
		t.Fatal("rendered title attribution is empty")
	}
	for line, count := range visibleByLine {
		if count == 0 {
			t.Fatalf("rendered title attribution line %d is empty", line+1)
		}
	}
}

func TestRenderTitleAttributionRejectsTextThatCannotFit(t *testing.T) {
	config, err := loadAttributionConfig("../..")
	if err != nil {
		t.Fatal(err)
	}
	if overlay, err := renderTitleAttribution("../..", config, "v"+strings.Repeat("1", 200)); err == nil || overlay != nil {
		t.Fatalf("oversized attribution returned overlay %v, error %v", overlay, err)
	}
}

func TestRenderTitleAttributionAcceptsGitTagPath(t *testing.T) {
	config, err := loadAttributionConfig("../..")
	if err != nil {
		t.Fatal(err)
	}
	if overlay, err := renderTitleAttribution("../..", config, "v1/release"); err != nil || overlay == nil {
		t.Fatalf("render legal Git tag: overlay %v, error %v", overlay, err)
	}
}

func TestRenderTitleAttributionAcceptsDirtySuffixBeyondCapacity(t *testing.T) {
	config, err := loadAttributionConfig("../..")
	if err != nil {
		t.Fatal(err)
	}
	if overlay, err := renderTitleAttribution("../..", config, "v1.0.2-alpha-dirty"); err != nil || overlay == nil {
		t.Fatalf("render dirty version: overlay %v, error %v", overlay, err)
	}
}

func TestParseAttributionConfigRejectsUnknownFields(t *testing.T) {
	data := []byte("format = \"zill-title-attribution\"\nversion = 1\ncredit = \"credit\"\nurl = \"example.test\"\nextra = true\n")
	if _, err := parseAttributionConfig(data); err == nil {
		t.Fatal("title attribution accepted an unknown field")
	}
}
