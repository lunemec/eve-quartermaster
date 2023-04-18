package bot

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/lunemec/eve-quartermaster/pkg/repository"
	"github.com/pkg/errors"
)

var requireRegex = regexp.MustCompile(`^(?P<number>[0-9]+)\s(?P<contract>[Aa]lliance|[Cc]orporation|[Cc]orp)\s(?P<name>.*)$`)

// requireHandler will be called every time a new
// message is created on any channel that the autenticated bot has access to.
func (b *quartermasterBot) requireHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Required list goes first so that we don't trigger both it and !require.
	if m.Content == "!require list" {
		b.log.Infow("Responding to !require list command", "channel_id", m.ChannelID)
		requiredDoctrines, err := b.repository.ReadAll()
		if err != nil {
			b.log.Errorw("error reading required in stock doctrines", "error", err)
			b.sendError(err, m.ChannelID)
			return
		}
		messages := requireListMessage(requiredDoctrines)
		if len(messages) == 0 {
			b.sendNoDoctrinesAddedMessage(m)
			return
		}
		for _, message := range messages {
			_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, message)
			if err != nil {
				b.log.Errorw("error sending message for !require list", "error", err)
			}
		}
		return
	}

	if strings.HasPrefix(m.Content, "!require") {
		b.log.Infow("Responding to !require command", "channel_id", m.ChannelID)
		// Format is: "!require NN alliance|corporation Doctrine name", example: "!require 10 Alliance Shield Drake"
		commandContent := strings.TrimPrefix(m.Content, "!require ")
		matches := requireRegex.FindAllStringSubmatch(commandContent, -1)

		if len(matches) == 0 || (len(matches) != 0 && len(matches[0]) != 4) {
			// Send back "unrecognised - format is ..."
			msg := fmt.Sprintf("unrecognised !require `%s`, the format is `!require N Alliance|Corp Some doctrine`", commandContent)
			_, err := b.discord.ChannelMessageSend(m.ChannelID, msg)
			if err != nil {
				b.log.Errorw("error responding to unknown !require", "error", err)
				return
			}
			return
		}

		doctrineName := matches[0][3]
		// It should be impossible to fail since we match with reges [0-9]
		requireStock, _ := strconv.Atoi(matches[0][1])
		contractOn, err := validateContractOn(strings.ToLower(matches[0][2]))
		if err != nil {
			b.sendError(err, m.ChannelID)
			return
		}
		doctrine, err := b.repository.Get(doctrineName)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			b.log.Errorw("error loading doctrine data", "error", err)

			b.sendError(err, m.ChannelID)
			return
		}
		doctrine.ContractedOn = contractOn
		doctrine.RequireStock = requireStock
		doctrine.Name = doctrineName
		err = b.repository.Set(doctrineName, doctrine)
		if err != nil {
			b.log.Errorw("error saving require in stock doctrine", "error", err)

			b.sendError(err, m.ChannelID)
			return
		}

		err = b.discord.MessageReactionAdd(m.ChannelID, m.ID, `👍`)
		if err != nil {
			b.log.Errorw("error reacting with :+1:", "error", err)
			return
		}
		return
	}
}

func requireListMessage(requiredDoctrines []repository.Doctrine) []*discordgo.MessageEmbed {
	var (
		partsCorporation, partsAlliance []string
	)

	sort.Slice(requiredDoctrines, func(i, j int) bool {
		return requiredDoctrines[i].Name < requiredDoctrines[j].Name
	})

	for _, doctrine := range filterDoctrines(requiredDoctrines, repository.Corporation) {
		partsCorporation = append(partsCorporation, fmt.Sprintf("**%s** %d", doctrine.Name, doctrine.RequireStock))
	}

	for _, doctrine := range filterDoctrines(requiredDoctrines, repository.Alliance) {
		partsAlliance = append(partsAlliance, fmt.Sprintf("**%s** %d", doctrine.Name, doctrine.RequireStock))
	}

	var (
		messages []*discordgo.MessageEmbed
	)

	if len(partsAlliance) > 0 {
		messageParts := splitMessageParts(partsAlliance, discordMaxDescriptionLength)
		for _, message := range messageParts {
			messages = append(messages, &discordgo.MessageEmbed{
				Title: "Target stock [Alliance]",
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
			messages = append(messages, &discordgo.MessageEmbed{
				Title: "Target stock [Corporation]",
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

func validateContractOn(input string) (repository.ContractedOn, error) {
	switch input {
	case "corp", "corporation":
		return repository.Corporation, nil
	case "alliance":
		return repository.Alliance, nil
	}

	return repository.ContractedOn(input), errors.Errorf("Unknown contract target: %s", input)
}
