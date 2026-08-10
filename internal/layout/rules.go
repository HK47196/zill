// SPDX-License-Identifier: GPL-3.0-or-later

package layout

// These are properties of the supported game's text renderers and fixed
// buffers. Message-to-consumer membership lives in release/layout/consumer-map.toml.
const (
	defaultAdvance            = 440
	c5Advance                 = 300
	equipmentFeedbackAdvance  = 240
	systemHelpAdvance         = 240
	profileAdvance            = 300
	profileMaxLines           = 8
	c5LinesPerPage            = 3
	c5MaxPages                = 9
	c5PageCapacity            = 256
	c20Capacity               = 768
	c22TotalCapacity          = 512
	c22PageCapacity           = 256
	c22MaxPages               = 9
	c22MaxLineBytes           = 56
	boundedLabelCapacity      = 28
	guildClientCapacity       = 17
	guildPostingCapacity      = 316
	guildRegionCapacity       = 152
	trapID                    = 1070079
	trapCapacity              = 104
	trapValueMaxBytes         = 11
	equipmentFeedbackCapacity = 109
)
