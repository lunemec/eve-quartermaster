package bot

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/antihax/goesi/esi"
	"github.com/bwmarrin/discordgo"
	"github.com/dustin/go-humanize"
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
		corporationDoctrines, soldCorporationDoctrines, allianceDoctrines, soldAllianceDoctrines, alerts, err := b.reportFull()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)

			b.sendError(err, m)
			return
		}
		corporationMessage, allianceMessage, alertMessage := b.reportFullMessage(
			corporationDoctrines,
			soldCorporationDoctrines,
			allianceDoctrines,
			soldAllianceDoctrines,
			alerts,
		)

		if allianceMessage == nil && corporationMessage == nil && alertMessage == nil {
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
		if alertMessage != nil {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				alertMessage,
			)
			if err != nil {
				b.log.Errorw("error sending alert report message",
					"error", err,
					"message", alertMessage,
				)
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
	[]esi.GetCorporationsCorporationIdContracts200Ok,
	error,
) {
	allContracts, err := b.loadContracts()
	if err != nil {
		return nil, nil, nil, nil, nil, errors.Wrap(err, "unable to load contracts")
	}

	corporationContracts, allianceContracts := b.filterAndGroupContracts(
		allContracts,
		statusOutstanding,
		typeItemExchange,
		true,
	)
	gotCorporationDoctrines := doctrinesAvailable(corporationContracts)
	gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
	requireAllDoctrines, err := b.repository.Read()
	if err != nil {
		return nil, nil, nil, nil, nil, errors.Wrap(err, "error reading required doctrines")
	}

	// Get list of finished contracts to see how many sell per month.
	finishedCorporationContracts, finishedAllianceContracts := b.filterAndGroupContracts(
		allContracts,
		statusFinished,
		typeItemExchange,
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
		b.filterAlertContracts(allContracts),
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
	alerts []esi.GetCorporationsCorporationIdContracts200Ok,
) (*discordgo.MessageEmbed, *discordgo.MessageEmbed, *discordgo.MessageEmbed) {
	var (
		partsCorporation, partsAlliance, partsAlerts []string
		msgOK                                        = ":small_blue_diamond: **%s** [%d/mo] - stocked %d, required %d"
		msgMissing                                   = ":small_orange_diamond: **%s** [%d/mo] - stocked %d, required %d"
		msgAlertExpired                              = "**%s**: By: **%s**, Expired: **%s**"
		msgAlert                                     = "**%s**: By: **%s**, Type: **%s**, Status: **%s**"
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

	for _, alert := range alerts {
		if alert.DateExpired.Before(time.Now()) {
			partsAlerts = append(partsAlerts, fmt.Sprintf(msgAlertExpired,
				alert.Title,
				b.idToName(alert.IssuerId),
				humanize.Time(alert.DateExpired),
			))
			continue
		}
		partsAlerts = append(partsAlerts, fmt.Sprintf(msgAlert,
			alert.Title,
			b.idToName(alert.IssuerId),
			alert.Type_,
			alert.Status,
		))
	}

	var (
		allianceMessage, corporationMessage, alertMessage *discordgo.MessageEmbed
		color                                             = 0x00ff00
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
	if len(alerts) != 0 {
		alertMessage = &discordgo.MessageEmbed{
			Thumbnail: &discordgo.MessageEmbedThumbnail{
				URL: "https://i.imgur.com/ZwUn8DI.jpg",
			},
			Color:       0xff0000,
			Description: strings.Join(partsAlerts, "\n"),
			Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
			Title:       ":x: Problematic contracts",
		}
	}

	return corporationMessage, allianceMessage, alertMessage
}
