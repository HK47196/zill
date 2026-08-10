// SPDX-License-Identifier: GPL-3.0-or-later

package release

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"github.com/HK47196/zill/internal/gamefmt/pspiso"
)

const (
	translatedISOName      = "zill-english.iso"
	translatedPatchName    = "zill-english.xdelta"
	supportedISOSize       = 728104960
	supportedISOSHA256     = "6e0827585c94694a3aeaca6322713a347cdce86bcbab768fa4b9d093f38d68fa"
	supportedXdeltaVersion = "Xdelta version 3.2.0,"
)

func openRetailISO(path string) (*pspiso.Image, pspiso.Manifest, error) {
	image, err := pspiso.Open(path)
	if err != nil {
		return nil, pspiso.Manifest{}, err
	}
	manifest := image.Manifest()
	if manifest.SourceSize != supportedISOSize || fmt.Sprintf("%x", manifest.SourceSHA256) != supportedISOSHA256 {
		_ = image.Close()
		return nil, pspiso.Manifest{}, fmt.Errorf("retail ISO is not supported ULJM05410 1.03: size %d, SHA-256 %x", manifest.SourceSize, manifest.SourceSHA256)
	}
	return image, manifest, nil
}

func authorTranslatedISO(outputPath string, image *pspiso.Image, manifest pspiso.Manifest, gameDir string) error {
	output, err := os.OpenFile(outputPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create staged translated ISO: %w", err)
	}
	tree := discPayloads{retail: image.PayloadFS(), game: os.DirFS(gameDir)}
	if err := pspiso.BuildModifiedFile(output, manifest, tree); err != nil {
		_ = output.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("author translated ISO: %w", err)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("close translated ISO: %w", err)
	}
	return nil
}

type discPayloads struct {
	retail fs.FS
	game   fs.FS
}

func (d discPayloads) Open(name string) (fs.File, error) {
	if name == "PSP_GAME" {
		return d.game.Open(".")
	}
	if strings.HasPrefix(name, "PSP_GAME/") {
		return d.game.Open(strings.TrimPrefix(name, "PSP_GAME/"))
	}
	return d.retail.Open(name)
}

func findXdelta() (string, error) {
	binary, err := exec.LookPath("xdelta3")
	if err != nil {
		return "", errors.New("xdelta3 3.2.0 is required for the maintainer build")
	}
	command := exec.Command(binary, "-V")
	command.Env = withoutEnvironment("XDELTA")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run xdelta3 -V: %w", err)
	}
	if !strings.HasPrefix(string(output), supportedXdeltaVersion) {
		return "", fmt.Errorf("unsupported xdelta3 version: %s", strings.TrimSpace(string(output)))
	}
	return binary, nil
}

func createAndVerifyPatch(binary, sourceISO, translatedISO, patchPath string) error {
	encode := exec.Command(binary,
		"-e", "-q", "-9", "-S", "djw", "-D", "-A",
		"-B", "67108864", "-W", "8388608", "-P", "262144", "-I", "32768",
		"-s", sourceISO, translatedISO, patchPath,
	)
	encode.Env = withoutEnvironment("XDELTA")
	if output, err := encode.CombinedOutput(); err != nil {
		_ = os.Remove(patchPath)
		return fmt.Errorf("create xdelta patch: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := verifyPatch(binary, sourceISO, translatedISO, patchPath); err != nil {
		_ = os.Remove(patchPath)
		return err
	}
	return nil
}

func verifyPatch(binary, sourceISO, translatedISO, patchPath string) error {
	decoded := exec.Command(binary, "-d", "-q", "-D", "-c", "-s", sourceISO, patchPath)
	decoded.Env = withoutEnvironment("XDELTA")
	var stderr bytes.Buffer
	decoded.Stderr = &stderr
	output, err := decoded.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open xdelta verification output: %w", err)
	}
	want, err := os.Open(translatedISO)
	if err != nil {
		return fmt.Errorf("open translated ISO for xdelta verification: %w", err)
	}
	defer want.Close()
	if err := decoded.Start(); err != nil {
		return fmt.Errorf("start xdelta verification: %w", err)
	}
	compareErr := compareDecoded(output, want)
	if compareErr != nil {
		_ = decoded.Process.Kill()
	}
	waitErr := decoded.Wait()
	if compareErr != nil {
		return fmt.Errorf("verify decoded xdelta output: %w", compareErr)
	}
	if waitErr != nil {
		return fmt.Errorf("decode xdelta patch: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func compareDecoded(decoded io.Reader, want io.Reader) error {
	decodedBuffer := make([]byte, 64*1024)
	wantBuffer := make([]byte, len(decodedBuffer))
	for {
		count, readErr := decoded.Read(decodedBuffer)
		if count > 0 {
			if _, err := io.ReadFull(want, wantBuffer[:count]); err != nil {
				return errors.New("decoded xdelta output is longer than the translated ISO")
			}
			if !bytes.Equal(decodedBuffer[:count], wantBuffer[:count]) {
				return errors.New("decoded xdelta output differs from the translated ISO")
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			break
		}
	}
	var extra [1]byte
	if count, err := want.Read(extra[:]); count != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return errors.New("decoded xdelta output is shorter than the translated ISO")
	}
	return nil
}

func withoutEnvironment(name string) []string {
	prefix := name + "="
	environment := os.Environ()
	filtered := environment[:0]
	for _, value := range environment {
		if !strings.HasPrefix(value, prefix) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
