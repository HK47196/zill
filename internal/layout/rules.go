// SPDX-License-Identifier: GPL-3.0-or-later

package layout

// These are properties of the supported game's text renderers and fixed
// buffers. Message-to-consumer membership lives in release/layout/consumer-map.toml.
const (
	defaultAdvance                       = 440
	objectiveAdviceAdvance               = 300
	characterCreationPromptAdvance       = 300
	characterCreationChoiceCapacityBytes = 31
	c5Advance                            = 300
	c5PortraitAdvance                    = 240
	chronicleAdvance                     = 300
	chronicleMaxLines                    = 10
	equipmentFeedbackAdvance             = 240
	systemHelpAdvance                    = 240
	profileAdvance                       = 300
	profileMaxLines                      = 8
	playerNameMaxCharacters              = 8
	playerNameMaxEncodedBytes            = playerNameMaxCharacters * 2
	c5LinesPerPage                       = 3
	c5MaxPages                           = 9
	c5PageBufferCapacityBytes            = 256
	c20GroupBufferCapacityBytes          = 768
	c22TotalBufferCapacityBytes          = 512
	c22PageBufferCapacityBytes           = 256
	c22MaxPages                          = 9
	c22MaxLineBytes                      = 56
	boundedLabelBufferCapacityBytes      = 28
	guildClientBufferCapacityBytes       = 17
	guildPostingBufferCapacityBytes      = 316
	guildRegionBufferCapacityBytes       = 152
	trapID                               = 1070079
	trapBufferCapacityBytes              = 104
	trapValueMaxBytes                    = 11
	equipmentFeedbackBufferCapacityBytes = 109
	chronicleEntryMaxPayloadBytes        = 764
	guildTextAdvance                     = 260
)
