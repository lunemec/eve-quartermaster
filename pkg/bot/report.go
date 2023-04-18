package bot

import (
	"fmt"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/pkg/errors"
)

// reportHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) reportHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Content == "!report full" {
		b.log.Infow("Responding to !report full", "channel_id", m.ChannelID)
		corporationDoctrines, soldCorporationDoctrines, allianceDoctrines, soldAllianceDoctrines, alerts, err := b.reportFull()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)

			b.sendError(err, m.ChannelID)
			return
		}
		messages := b.reportFullMessage(
			corporationDoctrines,
			soldCorporationDoctrines,
			allianceDoctrines,
			soldAllianceDoctrines,
			alerts,
		)

		if len(messages) == 0 {
			b.sendNoDoctrinesAddedMessage(m)
			return
		}

		for _, message := range messages {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				message,
			)
			if err != nil {
				b.log.Errorw("error sending message report message", "error", err)
			}
		}
		return
	}

	if m.Content == "!report" || m.Content == "!qm" {
		b.log.Infow("Responding to !qm command", "channel_id", m.ChannelID)
		missingCorporationDoctrines, missingAllianceDoctrines, allOnContract, err := b.reportMissing()
		if err != nil {
			b.log.Errorw("Error checking for missing doctrines",
				"error", err,
			)
			b.sendError(err, m.ChannelID)
			return
		}
		if allOnContract {
			b.sendAllOnContractMessage(m)
			return
		}

		messages := b.notifyMessage(missingCorporationDoctrines, missingAllianceDoctrines)
		if len(messages) == 0 {
			b.sendNoDoctrinesAddedMessage(m)
			return
		}
		for _, message := range messages {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				message,
			)
			if err != nil {
				b.log.Errorw("error sending report message", "error", err)
			}
		}
		return
	}
}

func (b *quartermasterBot) reportFull() (
	[]doctrineReport,
	map[string]int,
	[]doctrineReport,
	map[string]int,
	[]alertContract,
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
	requireAllDoctrines, err := b.repository.ReadAll()
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
		b.filterAlertContracts(requireAllDoctrines, allContracts),
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
	alerts []alertContract,
) []*discordgo.MessageEmbed {
	var (
		partsCorporation, partsAlliance, partsAlerts []string
		msgOK                                        = ":small_blue_diamond: **%s** [ƶ %.0fM, %d/mo] - stocked %d, required %d"
		msgMissing                                   = ":small_orange_diamond: **%s** [ƶ %.0fM, %d/mo] - stocked %d, required %d"
		msgAlert                                     = "**%s**: Reason: **%s** By: **%s**, Type: **%s**, Status: **%s**"
	)

	for _, doctrine := range allianceDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.RequireStock {
			msg = msgMissing
		}
		part := fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			float64(doctrine.doctrine.Price.Buy)/1000000,
			soldAllianceDoctrines[doctrine.doctrine.Name],
			doctrine.haveInStock,
			doctrine.doctrine.RequireStock,
		)
		partsAlliance = append(partsAlliance, part)
	}

	for _, doctrine := range corporationDoctrines {
		msg := msgOK
		if doctrine.haveInStock < doctrine.doctrine.RequireStock {
			msg = msgMissing
		}
		part := fmt.Sprintf(msg,
			doctrine.doctrine.Name,
			float64(doctrine.doctrine.Price.Buy)/1000000,
			soldCorporationDoctrines[doctrine.doctrine.Name],
			doctrine.haveInStock,
			doctrine.doctrine.RequireStock,
		)
		partsCorporation = append(partsCorporation, part)
	}

	for _, alert := range alerts {
		contract := alert.Contract
		part := fmt.Sprintf(msgAlert,
			contract.Title,
			alert.Reason,
			b.idToName(contract.IssuerId),
			contract.Type_,
			contract.Status,
		)
		partsAlerts = append(partsAlerts, part)
	}

	var (
		messages []*discordgo.MessageEmbed
		color    = 0x00ff00
	)
	if len(allianceDoctrines) != 0 {
		allianceMessages := splitMessageParts(partsAlliance, discordMaxDescriptionLength)
		for _, allianceMessage := range allianceMessages {
			messages = append(messages,
				&discordgo.MessageEmbed{
					Thumbnail: &discordgo.MessageEmbedThumbnail{
						URL: "https://i.imgur.com/ZwUn8DI.jpg",
					},
					Color:       color,
					Description: allianceMessage,
					Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
					Title:       ":scroll: Doctrines full report [Alliance]",
				},
			)
		}
	}
	if len(corporationDoctrines) != 0 {
		corporationMessages := splitMessageParts(partsCorporation, discordMaxDescriptionLength)
		for _, corporationMessage := range corporationMessages {
			messages = append(messages,
				&discordgo.MessageEmbed{
					Thumbnail: &discordgo.MessageEmbedThumbnail{
						URL: "https://i.imgur.com/ZwUn8DI.jpg",
					},
					Color:       color,
					Description: corporationMessage,
					Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
					Title:       ":scroll: Doctrines full report [Corporation]",
				},
			)
		}
	}
	if len(alerts) != 0 {
		alertsMessages := splitMessageParts(partsAlerts, discordMaxDescriptionLength)
		for _, allertMessage := range alertsMessages {
			messages = append(messages,
				&discordgo.MessageEmbed{
					Thumbnail: &discordgo.MessageEmbedThumbnail{
						URL: "https://i.imgur.com/ZwUn8DI.jpg",
					},
					Color:       0xff0000,
					Description: allertMessage,
					Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
					Title:       ":x: Problematic contracts",
				},
			)
		}
	}

	return messages
}
