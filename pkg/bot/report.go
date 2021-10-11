package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/pkg/errors"
)

// reportHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) reportHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!report full" {
		corporationDoctrines, soldCorporationDoctrines, allianceDoctrines, soldAllianceDoctrines, err := b.reportFull()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)

			b.sendError(err, m)
			return
		}
		corporationMessage, allianceMessage := b.reportFullMessage(
			corporationDoctrines,
			soldCorporationDoctrines,
			allianceDoctrines,
			soldAllianceDoctrines,
		)

		if allianceMessage == nil && corporationMessage == nil {
			b.sendNoDoctrinesAddedMessage(m)
			return
		}

		if allianceMessage != nil {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				allianceMessage,
			)
			if err != nil {
				b.log.Errorw("error sending alliance report message", "error", err)
			}
		}
		if corporationMessage != nil {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				corporationMessage,
			)
			if err != nil {
				b.log.Errorw("error sending corporation report message", "error", err)
			}
		}
		return
	}

	if m.Content == "!report" || m.Content == "!qm" {
		missingCorporationDoctrines, missingAllianceDoctrines, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)
			b.sendError(err, m)
			return
		}
		msg := b.notifyMessage(missingCorporationDoctrines, missingAllianceDoctrines)
		if msg == nil {
			b.sendNoDoctrinesAddedMessage(m)
			return
		}
		_, err = b.discord.ChannelMessageSendEmbed(
			m.ChannelID,
			msg,
		)
		if err != nil {
			b.log.Errorw("error sending report message", "error", err)
		}
		return
	}
}

func (b *quartermasterBot) reportFull() (
	[]doctrineReport,
	map[string]int,
	[]doctrineReport,
	map[string]int,
	error,
) {
	allContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, nil, nil, errors.Wrap(err, "unable to load contracts")
	}

	corporationContracts, allianceContracts := b.filterAndGroupContracts(
		allContracts,
		"outstanding",
		true,
	)
	gotCorporationDoctrines := doctrinesAvailable(corporationContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	requireAllDoctrines, err := b.repository.Read()
	if err != nil {
		return nil, nil, nil, nil, errors.Wrap(err, "error reading required doctrines")
	}

	// Get list of finished contracts to see how many sell per month.
	finishedCorporationContracts, finishedAllianceContracts := b.filterAndGroupContracts(
		allContracts,
		"finished",
		false,
	)
	// Group them by contract title.
	finishedCorporationDoctrines := doctrinesAvailable(finishedCorporationContracts)
	finishedAllianceDoctrines := doctrinesAvailable(finishedAllianceContracts)

	requireCorporationDoctrines := filterDoctrines(requireAllDoctrines, repository.Corporation)
	requireAllianceDoctrines := filterDoctrines(requireAllDoctrines, repository.Alliance)

	return b.fullDoctrines(requireCorporationDoctrines, gotCorporationDoctrines),
		b.soldDoctrines(requireCorporationDoctrines, finishedCorporationDoctrines),
		b.fullDoctrines(requireAllianceDoctrines, gotAllianceDoctrines),
		b.soldDoctrines(requireAllianceDoctrines, finishedAllianceDoctrines),
		nil
}

func (b *quartermasterBot) fullDoctrines(
	requireDoctrines []repository.Doctrine,
	gotDoctrines map[string]int,
) []doctrineReport {
	var doctrines []doctrineReport

	doctrinesDiff := diffDoctrines(requireDoctrines, gotDoctrines)
	for _, doctrine := range doctrinesDiff {
		doctrines = append(doctrines, doctrine)
	}

	sort.Slice(doctrines, func(i, j int) bool {
		return doctrines[i].doctrine.Name < doctrines[j].doctrine.Name
	})
	return doctrines
}

func (b *quartermasterBot) soldDoctrines(
	requireDoctrines []repository.Doctrine,
	gotDoctrines map[string]int,
) map[string]int {
	var doctrines = make(map[string]int)
	for _, requiredDoctrine := range requireDoctrines {
		if requiredDoctrine.RequireStock == 0 {
			continue
		}
		for doctrine, count := range gotDoctrines {
			namesEqual := compareDoctrineNames(requiredDoctrine.Name, doctrine)

			if namesEqual {
				doctrines[requiredDoctrine.Name] += count
			}
		}
	}
	return doctrines
}

func (b *quartermasterBot) reportFullMessage(
	corporationDoctrines []doctrineReport,
	soldCorporationDoctrines map[string]int,
	allianceDoctrines []doctrineReport,
	soldAllianceDoctrines map[string]int,
) (*discordgo.MessageEmbed, *discordgo.MessageEmbed) {
	var (
		partsCorporation, partsAlliance []string
		msgOK                           = ":small_blue_diamond: **%s** [%d/mo] - stocked %d, required %d"
		msgMissing                      = ":small_orange_diamond: **%s** [%d/mo] - stocked %d, required %d"
	)

	for _, doctrine := range allianceDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.RequireStock {
			msg = msgMissing
		}
		partsAlliance = append(partsAlliance, fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			soldAllianceDoctrines[doctrine.doctrine.Name],
			doctrine.haveInStock,
			doctrine.doctrine.RequireStock,
		))
	}

	for _, doctrine := range corporationDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.RequireStock {
			msg = msgMissing
		}
		partsCorporation = append(partsCorporation, fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			soldCorporationDoctrines[doctrine.doctrine.Name],
			doctrine.haveInStock,
			doctrine.doctrine.RequireStock,
		))
	}

	var (
		allianceMessage, corporationMessage *discordgo.MessageEmbed
		color                               = 0x00ff00
	)
	if len(allianceDoctrines) != 0 {
		allianceMessage = &discordgo.MessageEmbed{
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: "https://i.imgur.com/ZwUn8DI.jpg",
			},
			Color:       color,
			Description: strings.Join(partsAlliance, "\n"),
			Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
			Title:       ":scroll: Alliance doctrines full report",
		}
	}
	if len(corporationDoctrines) != 0 {
		corporationMessage = &discordgo.MessageEmbed{
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: "https://i.imgur.com/ZwUn8DI.jpg",
			},
			Color:       color,
			Description: strings.Join(partsCorporation, "\n"),
			Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
			Title:       ":scroll: Corporation doctrines full report",
		}
	}

	return corporationMessage, allianceMessage
}
