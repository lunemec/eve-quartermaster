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
	// Ignore all messages created by the bot itself.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Required list goes first so that we don't trigger both it and !require.
	if m.Content == "!require list" {
		requiredDoctrines, err := b.repository.Read()
		if err != nil {
			b.log.Errorw("error reading required in stock doctrines", "error", err)
			b.sendError(err, m)
			return
		}
		_, err = b.discord.ChannelMessageSendEmbed(m.ChannelID, requireListMessage(requiredDoctrines))
		if err != nil {
			b.log.Errorw("error sending message for !require list", "error", err)
			return
		}
		return
	}

	if strings.HasPrefix(m.Content, "!require") {
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
			b.sendError(err, m)
			return
		}

		err = b.repository.Set(doctrineName, requireStock, contractOn)
		if err != nil {
			b.log.Errorw("error saving require in stock doctrine", "error", err)

			b.sendError(err, m)
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

func requireListMessage(requiredDoctrines []repository.Doctrine) *discordgo.MessageEmbed {
	var (
		partsCorporation, partsAlliance []string
	)

	sort.Slice(requiredDoctrines, func(i, j int) bool {
		return requiredDoctrines[i].Name < requiredDoctrines[j].Name
	})

	for _, doctrine := range filterDoctrines(requiredDoctrines, repository.Corporation) {
		partsCorporation = append(partsCorporation, fmt.Sprintf("%d %s", doctrine.RequireStock, doctrine.Name))
	}

	for _, doctrine := range filterDoctrines(requiredDoctrines, repository.Alliance) {
		partsAlliance = append(partsAlliance, fmt.Sprintf("%d %s", doctrine.RequireStock, doctrine.Name))
	}

	var msg = "Nothing has been added yet, add items using `!require` or see `!help`."
	if len(partsAlliance) != 0 || len(partsCorporation) != 0 {
		msg = ""
		if len(partsAlliance) != 0 {
			msg += fmt.Sprintf("**Alliance contracts**\n```\n%s\n```\n", strings.Join(partsAlliance, "\n"))
		}
		if len(partsCorporation) != 0 {
			msg += fmt.Sprintf("**Corporation contracts**\n```\n%s\n```\n", strings.Join(partsCorporation, "\n"))
		}
	}

	return &discordgo.MessageEmbed{
		Title: "Target stock",
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://i.imgur.com/ZwUn8DI.jpg",
		},
		Color:       0x00ff00,
		Description: msg,
		Timestamp:   time.Now().Format(time.RFC3339), // Discord wants ISO8601; RFC3339 is an extension of ISO8601 and should be completely compatible.
	}
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
