// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux

package paa

import "os"

func platformFileIdentity(os.FileInfo) (int64, uint64, uint64) {
	return 0, 0, 0
}
