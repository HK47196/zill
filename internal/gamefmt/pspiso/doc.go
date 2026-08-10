// SPDX-License-Identifier: GPL-3.0-or-later

// Package pspiso reads and describes the ISO 9660 profile used by PSP games.
//
// It is deliberately layout-aware. A Manifest retains the metadata required
// to author a separate image with the same sector layout, but never keeps file
// payloads or an open handle to the source image. Build proves the untouched
// round trip. BuildModified permits changed file bytes and deterministically
// reflows growing extents while preserving the source ordering and alignment.
package pspiso
