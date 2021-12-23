package discord

import (
	"fmt"
	"sort"
	"time"

	"github.com/bwmarrin/discordgo"
)

// stockHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (h *discordHandler) stockHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!stock" {
		b.log.Infow("Responding to !stock command", "channel_id", m.ChannelID)
		allContracts, err := b.loadContracts()

		if err != nil {
			b.log.Errorw("error loading ESI contracts", "error", err)
			b.sendError(err, m)
			return
		}
		corporationContracts, allianceContracts := b.filterAndGroupContracts(
			allContracts,
			statusOutstanding,
			typeItemExchange,
			true,
		)
		gotCorporationDoctrines := doctrinesAvailable(corporationContracts)
		gotAllianceDoctrines := doctrinesAvailable(allianceContracts)
		stockMessages := stockMessage(gotCorporationDoctrines, gotAllianceDoctrines)
		for _, message := range stockMessages {
			_, err = b.discord.ChannelMessageSendEmbed(
				m.ChannelID,
				message,
			)
			if err != nil {
				b.log.Errorw("error sending message for !stock", "error", err)
			}
		}
		return
	}
}

func stockMessage(corporationDoctrines, allianceDoctrines map[string]int) []*discordgo.MessageEmbed {
	var (
		namesCorporation, namesAlliance []string // used for sorting by name
		partsCorporation, partsAlliance []string
	)

	for haveDoctrine := range corporationDoctrines {
		namesCorporation = append(namesCorporation, haveDoctrine)
	}
	sort.Strings(namesCorporation)

	for haveDoctrine := range allianceDoctrines {
		namesAlliance = append(namesAlliance, haveDoctrine)
	}
	sort.Strings(namesAlliance)

	for _, name := range namesCorporation {
		if name == "" {
			continue
		}
		partsCorporation = append(partsCorporation, fmt.Sprintf("**%s** %d", name, corporationDoctrines[name]))
	}

	for _, name := range namesAlliance {
		if name == "" {
			continue
		}
		partsAlliance = append(partsAlliance, fmt.Sprintf("**%s** %d", name, allianceDoctrines[name]))
	}

	var messages []*discordgo.MessageEmbed

	if len(partsAlliance) > 0 {
		messageParts := splitMessageParts(partsAlliance, discordMaxDescriptionLength)
		for _, message := range messageParts {
			messages = append(messages,
				&discordgo.MessageEmbed{
					Title: "On contract [Alliance]",
					Thumbnail: &discordgo.MessageEmbedThumbnail{
						URL: "https://i.imgur.com/ZwUn8DI.jpg",
					},
					Color:       0x00ff00,
					Description: message,
					Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
				},
			)
		}
	}

	if len(partsCorporation) > 0 {
		messageParts := splitMessageParts(partsCorporation, discordMaxDescriptionLength)
		for _, message := range messageParts {
			messages = append(messages,
				&discordgo.MessageEmbed{
					Title: "On contract [Corporation]",
					Thumbnail: &discordgo.MessageEmbedThumbnail{
						URL: "https://i.imgur.com/ZwUn8DI.jpg",
					},
					Color:       0x00ff00,
					Description: message,
					Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
				},
			)
		}
	}

	return messages
}
